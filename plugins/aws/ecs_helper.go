/*
Copyright 2018 Turbine Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/arrays/dedupe"
	"github.com/turbinelabs/nonstdlib/arrays/indexof"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/nonstdlib/ptr"
)

// ecsMeta tracks the clusters available in an ECS deployment. Additionally,
// for a subset of clusters that rotor is watching, pulls configured
// services along with the tasks those services are bound to.
type ecsMeta struct {
	// clusters contains a list of available clusters (short name)
	clusters []string

	// clusterSvcs maps from a cluster short name to a list of service ARNs defined
	// for it
	clusterSvcs map[string][]arn

	// services maps from cluster -> service ARN -> svcDefn
	services map[string]map[arn]svcDefn

	// tasks maps task ARN to a taskDefn
	tasks map[arn]taskDefn
}

type containerBindTemplate struct {
	// cfg specifies the settings in which this cluter meta was created. Mostly
	// it contains the cluster tag and cluster port tags so that we know how to
	// extract data from labels.
	cfg ecsSettings

	// the short name cluster
	cluster string

	// service ARN that we found to contain the appropriate tag
	service arn

	// task definition ARN
	task arn

	// name of the container with a matching label
	container string

	// svcs contains the service names extracted from the tbn cluster label
	svcs []string

	// ports contains the service ports extracted from the tbn cluster label
	ports []int

	// docker labels
	labels map[string]*string
}

func parseLabel(s string) ([]string, []int, error) {
	sps := strings.Split(s, ",")
	ls := len(sps)
	services := make([]string, 0, ls)
	ports := make([]int, 0, ls)

	for _, sp := range sps {
		el := strings.Split(sp, ":")
		if len(el) != 2 {
			return nil, nil, fmt.Errorf("'%s' must be in the format service:port", sp)
		}
		svc := strings.TrimSpace(el[0])
		if svc == "" {
			return nil, nil, fmt.Errorf("'%s' contains no service declaration", sp)
		}
		port, err := strconv.Atoi(el[1])
		if err != nil {
			return nil, nil, fmt.Errorf("'%s' has an invalid port binding: %s", sp, err.Error())
		}
		services = append(services, svc)
		ports = append(ports, port)
	}

	return services, ports, nil
}

type portMappings []portMapping

// mapsTo walks a collection of portMapping and returns all host ports defined
// that map to a provided container port.
func (m portMappings) mapsTo(containerPort int) []int {
	results := []int{}
	for _, pm := range m {
		if pm.container == containerPort {
			results = append(results, pm.host)
		}
	}

	return results
}

type portMapping struct {
	container int
	host      int
}

// portMappings examines the ContainerDefinition associated with this bind template
// and returns the container:host port mappings as defined in ECS metadata.
func (cbt containerBindTemplate) portMappings(st ecsState) (portMappings, error) {
	task := st.meta.tasks[cbt.task]
	cdef := task.findContainerDefn(cbt.container)
	if cdef == nil {
		return nil, fmt.Errorf(
			"Could not find %s.%s container %s",
			cbt.cluster,
			cbt.service,
			cbt.container)
	}

	mappings := []portMapping{}
	for _, pm := range cdef.PortMappings {
		newPM := portMapping{}

		if pm == nil {
			continue
		}
		newPM.host = int(ptr.Int64Value(pm.HostPort))

		cport := int(ptr.Int64Value(pm.ContainerPort))
		if cport == 0 {
			return nil, fmt.Errorf("no container port mapping found in container definition")
		}
		newPM.container = cport

		mappings = append(mappings, newPM)
	}

	return mappings, nil
}

// identifyTaggedItems examines all known containers and return a collection of
// templates that we can use to construct Turbine Labs cluster instances.
func (m ecsMeta) identifyTaggedItems(cfg ecsSettings) []containerBindTemplate {
	result := []containerBindTemplate{}
	addTmpl := func(cbt containerBindTemplate) { result = append(result, cbt) }

	for _, cluster := range m.clusters {
		if _, ok := m.services[cluster]; !ok {
			m.services[cluster] = map[arn]svcDefn{}
		}

		for _, svc := range m.clusterSvcs[cluster] {
			sdef, ok := m.services[cluster][svc]
			if !ok {
				console.Error().Printf("Could not find service definition for %s.%s", cluster, svc)
				continue
			}

			tarn := arnValue(sdef.TaskDefinition)
			if tarn == "" {
				console.Error().Printf("Could not find task ARN for service %s.%s", cluster, svc)
				continue
			}

			tdef, ok := m.tasks[tarn]
			if !ok {
				console.Error().Printf(
					"Could not find task definition for task %s (in %s.%s)", tarn, cluster, svc)
				continue
			}

			for _, cdef := range tdef.ContainerDefinitions {
				if cdef == nil {
					continue
				}
				labels := cdef.DockerLabels
				for k := range labels {
					if k == cfg.clusterTag {
						lbl := ptr.StringValue(labels[k])
						s, p, err := parseLabel(lbl)
						if err != nil {
							console.Error().Printf("Could not parse %s %s: %s", tarn, lbl, err.Error())
							continue
						}
						addTmpl(containerBindTemplate{
							cfg,
							cluster,
							svc,
							tarn,
							ptr.StringValue(cdef.Name),
							s,
							p,
							labels,
						})
						break
					}
				}
			}
		}
	}

	return result
}

// validate takes a containerBindTemplate and verifies that it is reasonable when
// compared to the known state of the examined cluster definitions.
//
// Specifically this checks that for each service:port combination there
// is a corresponding container port exposed in the task definition. Also
// we ensure that each service is associted with an expected container
// port.
//
// An error is returned if either of these assumptions do not hold.
func (m ecsState) validate(cbt containerBindTemplate) error {
	svcs := cbt.svcs
	sc := len(svcs)

	ports := cbt.ports
	pc := len(ports)

	mappings, err := cbt.portMappings(m)
	if err != nil {
		return err
	}

	// multiple services defined, we require 1:1 binding to a container port
	if sc != pc {
		return errors.New("each service should have one associated port")
	}

	// has cluster
	if indexof.String(m.meta.clusters, cbt.cluster) == -1 {
		return fmt.Errorf("cluster %s not found", cbt.cluster)
	}
	// has svc
	if _, ok := m.meta.clusterSvcs[cbt.cluster]; !ok {
		return fmt.Errorf("no services defined for %s", cbt.cluster)
	}
	if indexOfArn(m.meta.clusterSvcs[cbt.cluster], cbt.service) == -1 {
		return fmt.Errorf("no service %s defined for cluster %s", cbt.service, cbt.cluster)
	}
	// has task
	if _, ok := m.meta.tasks[cbt.task]; !ok {
		return fmt.Errorf("no task %s found", cbt.task)
	}

	seen := map[int]bool{}
	// walk over the container ports and ensure each one has mapping to some host port
	for _, cp := range ports {
		if _, ok := seen[cp]; ok {
			continue
		}
		if len(mappings.mapsTo(cp)) < 1 {
			return fmt.Errorf(
				"%s container in %s did not contain a port mapping for %d",
				cbt.container,
				cbt.task,
				cp)
		}
		seen[cp] = true
	}

	return nil
}

// ecsRunning tracks data abotu running task and container instances as well as
// EC2 hosts that have been provisioned to support them. It's populated via
// populateRunning.
type ecsRunning struct {
	// svcTasks stores a cluster's services (by ARN) to the list of Task instance
	// ARNs running to support it i.e. cluster -> (svc_arn -> []task_instance_arn)
	svcTasks map[string]map[arn][]arn

	// taskInstances maps from a Task instance ARN to the actual object
	// description.
	taskInstances map[arn]taskInst

	// containerInstances maps from a ContainerInstance ARN to the actual
	// object description.
	containerInstances map[arn]containerInst

	// ec2Hosts maps from an EC2 instance ID to an object representing that
	// instance.
	ec2Hosts map[string]ec2Instance
}

// getTaskARNs returns an ARN for each task running on a cluster in support of
// the specified task
func (r ecsRunning) getTaskARNs(cluster string, svc arn) []arn {
	svcTaskMap, ok := r.svcTasks[cluster]
	if !ok {
		return nil
	}

	return svcTaskMap[svc]
}

// findHostPort walks the network bindings associated with a container and
// identifies a port on the executing host that forwards to a destination
// within the container. If multiple host ports forward to the same container
// port each will be assigned at least once and then selected at random.
//
// An error is returned if no forwarding port is found.
func findHostPort(
	c *ecs.Container,
	destPort int,
	usePort func(carn arn, port int),
	isUsed func(carn arn, port int) bool,
) (int, error) {
	carn := arn("")
	if c != nil {
		carn = arnValue(c.ContainerArn)
	} else {
		console.Error().Printf("provided container was nil")
	}
	if carn != "" {
		viable := []int{}
		for _, nb := range c.NetworkBindings {
			cp := int(ptr.Int64Value(nb.ContainerPort))
			hp := int(ptr.Int64Value(nb.HostPort))
			if cp != destPort {
				continue
			}
			if !isUsed(carn, hp) {
				usePort(carn, hp)
				return hp, nil
			} else {
				viable = append(viable, hp)
			}
		}

		lv := len(viable)
		if lv != 0 {
			return viable[rand.Intn(lv)], nil
		}
	} else {
		console.Error().Printf("%v had no container ARN", c)
	}

	return 0, fmt.Errorf("could not bind to port %d in container '%s'", destPort, carn)
}

// bindClusters takes a snapshot of an ECS environment (one or more clusters)
// and a set of metadata describing potential API cluster instances (c.f.
// containerBindTemplate docs). Clusters and instances are produced using the following
// methodology:
//
//    For each template collect the Task instances running in support of the
//    ECS cluster/service pair.
//
//    Skip running Tasks that do not match the template's task definition.
//
//    Determine the container instance data and the specific container associated
//    this template.
//
//    For each service:port mapping the template indicates should be present
//    within this container found the port on the host that it is bound to.
//
//    Take the discovered host port from above and the EC2 instance specified
//    from the Container Instance data above and use this to create an API
//    Cluster instance. Attach to this instance a bunch of metadata from
//    the binding process.
//
//    The API Cluster this instance is added to in taken from the service
//    specificed in the service:port pair.
//
// If at any point we hit an error we'll log it and continue execution.
func bindClusters(clusterTag string, state ecsState, tmpls []containerBindTemplate) []api.Cluster {
	// cluster (service) name as specified by clusterTag mapped to the API cluster
	// that is collecting instances
	clusterMap := map[string]*api.Cluster{}
	getCluster := func(name string) *api.Cluster {
		if _, ok := clusterMap[name]; !ok {
			clusterMap[name] = &api.Cluster{}
			clusterMap[name].Name = name
		}
		return clusterMap[name]
	}

	// containerinstance ARN -> ports that have been bound to a Cluster instance
	usedPorts := map[arn]map[int]bool{}
	usePort := func(carn arn, port int) {
		if _, ok := usedPorts[carn]; !ok {
			usedPorts[carn] = map[int]bool{}
		}
		usedPorts[carn][port] = true
	}
	isUsed := func(carn arn, port int) bool {
		if _, ok := usedPorts[carn]; !ok {
			return false
		}
		return usedPorts[carn][port]
	}

	findPort := func(c *ecs.Container, destPort int) (int, error) {
		return findHostPort(c, destPort, usePort, isUsed)
	}

	missing := func(cluster string, tarn arn, desc string) {
		console.Error().Printf(
			"%s / %s disconnect between layout and live: %s not found", cluster, tarn, desc)
	}

	for _, tmpl := range tmpls {
		taskARNs := state.live.getTaskARNs(tmpl.cluster, tmpl.service)

		for i, tmplSvc := range tmpl.svcs {
			// the associated service port
			cSvcPort := tmpl.ports[i]

			// find the API Cluster we're adding instances to
			c := getCluster(tmplSvc)

			// look at each running task as it will create one or more instances
			for _, tarn := range taskARNs {
				// the running task
				tinst, ok := state.live.taskInstances[tarn]
				if !ok {
					missing(tmpl.cluster, tmpl.service, string(tarn))
					continue
				}

				// ... it's definition
				tdefarn := arnValue(tinst.TaskDefinitionArn)
				// ... and make sure it matches the task we're trying to match.
				if tdefarn != tmpl.task {
					continue
				}

				// Find the container host's metadata
				ciarn := arnValue(tinst.ContainerInstanceArn)
				cinst, ok := state.live.containerInstances[ciarn]
				if !ok {
					missing(tmpl.cluster, tmpl.service, string(ciarn))
					continue
				}

				// a container instance is bound to a particular EC2 host
				ec2id := ptr.StringValue(cinst.Ec2InstanceId)
				ec2inst, ok := state.live.ec2Hosts[ec2id]
				if !ok {
					missing(tmpl.cluster, tmpl.service, fmt.Sprintf("EC2 host %s", ec2id))
					continue
				}

				// ...with an IP address
				ec2Host := ptr.StringValue(ec2inst.PrivateIpAddress)

				// grab the container our template is for out of the task instance
				container := tinst.getContainer(tmpl.container)
				if container == nil {
					missing(
						tmpl.cluster,
						tmpl.service,
						fmt.Sprintf("container %s in task %s", tmpl.container, tarn))
					continue
				}

				// find a host port bound to the container service port
				hostPort, err := findPort(container, cSvcPort)
				if err != nil {
					missing(
						tmpl.cluster,
						tmpl.service,
						fmt.Sprintf("container port %d not exposed on host %s", cSvcPort, container))
					console.Error().Printf(err.Error())
					continue
				}

				c.Instances = append(
					c.Instances,
					mkInstance(clusterTag, ec2id, ec2Host, hostPort, tmpl, ciarn, tarn))
			}
		}
	}

	results := []api.Cluster{}
	for _, v := range clusterMap {
		results = append(results, *v)
	}

	return results
}

func mkInstance(
	clusterTag,
	ec2id,
	ip string,
	port int,
	cbt containerBindTemplate,
	containerInstance,
	task arn,
) api.Instance {
	metadata := map[string]string{
		"ecs-cluster":            cbt.cluster,
		"ecs-service":            extractSN(cbt.service),
		"ecs-service-arn":        string(cbt.service),
		"ecs-task-definition":    string(cbt.task),
		"ecs-task-container":     cbt.container,
		"ecs-container-instance": string(containerInstance),
		"ecs-task-instance":      string(task),
		"ec2-instance-id":        ec2id,
	}

	for k, vp := range cbt.labels {
		if k == clusterTag {
			continue
		}
		if _, ok := metadata[k]; !ok {
			metadata[k] = ptr.StringValue(vp)
		} else {
			console.Error().Printf("duplicate values for docker label %v; dropping value '%v'", k, ptr.StringValue(vp))
		}
	}

	return api.Instance{
		Host:     ip,
		Port:     port,
		Metadata: api.MetadataFromMap(metadata),
	}
}

type ecsState struct {
	meta ecsMeta
	live ecsRunning
}

// populateMeta examines the specified clusters to determine the layout of
// services and tasks that may contain API Cluster instances when they are
// scheduled.
//
// Attempts are made to minimize number of round trips by collecting all
// services within a cluster before requesting definitions.
func populateMeta(aws awsClient, state *ecsState) error {
	for _, cluster := range state.meta.clusters {
		if _, ok := state.meta.services[cluster]; !ok {
			state.meta.services[cluster] = map[arn]svcDefn{}
		}

		// 1. get all services defined for this service
		svcs, err := aws.ListServices(cluster)
		if err != nil {
			return err
		}

		svcARNs := make([]arn, 0, len(svcs))
		for _, v := range svcs {
			svcARNs = append(svcARNs, v)
		}

		state.meta.clusterSvcs[cluster] = svcARNs
		console.Debug().Printf("cluster '%s' svcs %s", cluster, svcARNs)

		// 2. look up services associated with this cluster
		defns, err := aws.ServiceDefinitions(cluster, svcARNs)
		if err != nil {
			return err
		}
		// ...and merge it into our global list of services
		for k, v := range defns {
			console.Debug().Printf("  adding service %s", k)
			state.meta.services[cluster][arn(k)] = v
		}

		console.Debug().Printf("Getting Task Definitions")
		// 3. Look up task definitions for each service
		for sarn, sdef := range state.meta.services[cluster] {
			console.Debug().Printf("  For service %s", sarn)
			tarn := arnValue(sdef.TaskDefinition)
			if tarn == "" {
				console.Error().Printf("    %s contained no task definition", sarn)
				continue
			}
			if _, hasT := state.meta.tasks[tarn]; hasT {
				console.Debug().Printf("    Skipping %s", tarn)
				continue
			}

			tdef, err := aws.TaskDefinition(tarn)
			if err != nil {
				return err
			}
			console.Debug().Printf("    got task %s", tarn)
			state.meta.tasks[tarn] = tdef
		}
	}

	return nil
}

// getRunningTasks will examine the task ARN executing in the current cluster
// and then look up the associated task instance saving the arn -> instance
// mapping into state.live.taskInstances
func getRunningTasks(
	aws awsClient,
	state *ecsState,
	currentCluster string,
) (map[arn]taskInst, error) {
	clusterTasks := map[arn]taskInst{}
	for _, svc := range state.meta.clusterSvcs[currentCluster] {
		console.Debug().Printf("%s.%s", currentCluster, svc)
		// 1. first get the running tasks
		svcTasksARNs, err := aws.ListTasks(currentCluster, svc)
		if err != nil {
			return nil, err
		}
		for _, a := range svcTasksARNs {
			console.Debug().Printf("  %s", a)
		}
		if _, exists := state.live.svcTasks[currentCluster]; !exists {
			state.live.svcTasks[currentCluster] = map[arn][]arn{}
		}
		state.live.svcTasks[currentCluster][svc] = svcTasksARNs

		// 2. ...and get details about each task
		svcTasksARNs = sortDedupeARNs(svcTasksARNs)
		newClusterTasks, err := aws.GetTasks(clusterTasks, currentCluster, svcTasksARNs...)
		if err != nil {
			return nil, err
		}
		for k, v := range newClusterTasks {
			clusterTasks[k] = v
			if _, ok := state.live.taskInstances[k]; ok {
				continue
			}
			state.live.taskInstances[k] = v
		}
	}

	return clusterTasks, nil
}

// getContainerInstances takes a list of task instances that are running within
// a cluster and finds the associated container instances that support them.
// This data is loaded into the state.live.containerInstances map.
func getContainerInstances(
	aws awsClient,
	state *ecsState,
	cluster string,
	clusterTasks map[arn]taskInst,
) error {
	// Having collected all the running task data for this cluster pull
	// container instance IDs
	ciids := []arn{}
	for tarn, t := range clusterTasks {
		carn := arnValue(t.ContainerInstanceArn)
		if carn == "" {
			console.Error().Printf("Task %s contained container instance with no ARN", tarn)
			continue
		}
		ciids = append(ciids, carn)
	}
	// ...and get a description of them.
	ciids = sortDedupeARNs(ciids)
	newContainerInsts, err := aws.GetContainerInstances(
		state.live.containerInstances, cluster, ciids...)
	if err != nil {
		return err
	}
	for k, v := range newContainerInsts {
		state.live.containerInstances[k] = v
	}

	return nil
}

// getEC2Hosts walks the containers known in state.live.containerInstances and
// gathers informatino about the associated EC2 hosts on which the containers
// are executing. The resulting data is stored in state.live.ec2Hosts.
func getEC2Hosts(aws awsClient, state *ecsState) error {
	// Now extract all ec2 instance IDs
	ec2IDs := []string{}
	for cid, ci := range state.live.containerInstances {
		id := ptr.StringValue(ci.Ec2InstanceId)
		if id == "" {
			console.Error().Printf("Container %s did not contain a EC2 Instance ID", cid)
			continue
		}
		ec2IDs = append(ec2IDs, id)
	}

	sort.Strings(ec2IDs)
	ec2IDs = dedupe.Strings(ec2IDs)
	hosts, err := aws.GetEC2Instances(ec2IDs...)
	if err != nil {
		return err
	}
	state.live.ec2Hosts = hosts

	return nil
}

// populateRunning looks at the ECS cluster layout information collected from
// populateMeta and requests all running tasks organizing them to facilitate
// producing API Cluster instances by binding containerBindTemplate templates
// running ECS Container instances.
func populateRunning(aws awsClient, state *ecsState) error {
	// Collect data across all clusters being queried
	for _, cluster := range state.meta.clusters {
		clusterTasks, err := getRunningTasks(aws, state, cluster)
		if err != nil {
			return err
		}

		if err = getContainerInstances(aws, state, cluster, clusterTasks); err != nil {
			return err
		}
	}

	// ...then fetch any associated ec2 hosts
	return getEC2Hosts(aws, state)
}

func emptyECSState() ecsState {
	return ecsState{
		ecsMeta{
			[]string{},
			map[string][]arn{},
			map[string]map[arn]svcDefn{},
			map[arn]taskDefn{},
		},
		ecsRunning{
			map[string]map[arn][]arn{},
			map[arn]taskInst{},
			map[arn]containerInst{},
			map[string]ec2Instance{},
		},
	}
}

// NewECSState constructs a snapshot view of a collection of clusters within
// ECS. If errors are hit they are returned with an empty ecsState object.
func NewECSState(aws awsClient, clusters []string) (ecsState, error) {
	ecs := emptyECSState()

	// Take the clusters we're examining as a given
	ecs.meta.clusters = clusters

	err := populateMeta(aws, &ecs)
	if err != nil {
		return ecsState{}, err
	}

	err = populateRunning(aws, &ecs)
	if err != nil {
		return ecsState{}, err
	}

	return ecs, nil
}

// taskDefn is a template describing a Task that may be run as a service within
// a cluster.
type taskDefn struct{ *ecs.TaskDefinition }

// findContainerDefn look for the definition of a containr by a specific name
// within the definition of a task.
func (td taskDefn) findContainerDefn(name string) *ecs.ContainerDefinition {
	for _, cd := range td.ContainerDefinitions {
		if name == ptr.StringValue(cd.Name) {
			return cd
		}
	}

	return nil
}

// svcDefn is a template describing how a TaskDefinition is bound to a cluster
// with specific scaling properties (autoscale, ELB, etc.).
type svcDefn struct{ *ecs.Service }

// taskInst is an instance of a of a TaskDefinition that has been instantiated
// to support a Service. It has one or more Containers and executes within a
// ContainerInstance
type taskInst struct{ *ecs.Task }

// getContainer gets the running Container instance of the specified name
// from a running Task instance. If no container of that name is present
// returns nil.
func (t taskInst) getContainer(named string) *ecs.Container {
	for _, c := range t.Containers {
		if c != nil && ptr.StringValue(c.Name) == named {
			return c
		}
	}

	return nil
}

// containerInst contains metadata about a Container (in the docker sense) that
// is executing on some EC2 host.
type containerInst struct{ *ecs.ContainerInstance }

// ec2Instance contains metadata about a host in EC2 that is part of the ECS
// cluster and is (presumably) running some containers.
type ec2Instance struct{ *ec2.Instance }

// extractSN returns a short name from an ARN
func extractSN(srcARN arn) string {
	src := string(srcARN)
	idx := strings.LastIndex(src, "/")
	if idx == -1 {
		return ""
	}
	return src[idx+1:]
}

// extractSNARNMap converts a list of ARNs into a short name -> ARN map.
func extractSNARNMap(typ string, results map[string]arn, input []*string) error {
	for _, sptr := range input {
		if sptr == nil {
			return fmt.Errorf("Unexpectd nil walking ECS %s", typ)
		}

		s := arnValue(sptr)
		cname := extractSN(s)
		if _, dupe := results[cname]; dupe {
			return fmt.Errorf("Duplicate ECS %s found: %s", typ, s)
		}
		results[cname] = s
	}

	return nil
}

func indexOfArn(slc []arn, el arn) int {
	return indexof.IndexOf(
		len(slc),
		func(i int) bool { return slc[i] == el },
	)
}

func sortDedupeARNs(as []arn) []arn {
	ss := a2s(as)
	sort.Strings(ss)
	ss = dedupe.Strings(ss)
	return s2a(ss)
}

func arnValue(s *string) arn {
	return arn(ptr.StringValue(s))
}
