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

// Package aws provides integrations with Amazon EC2 and ECS. See
// "rotor help aws" and "rotor help ecs" for usage.
package aws

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/flag/usage"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbnstrings "github.com/turbinelabs/nonstdlib/strings"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
)

const (
	defaultClusterTagNamespace = "tbn:cluster"
	delimiter                  = ":"

	awsDescription = `Connects to the AWS API in a given region and
updates Clusters stored in the Turbine Labs API at startup and periodically
thereafter.

EC2 instance tags are used to determine to which clusters an instance belongs.
An EC2 instance may belong to multiple clusters, serving traffic on multiple
ports. Cluster membership on a port is declared with a tag, of the form:

    "<namespace>:<cluster-name>:<port>"=""

The port must be numeric, and the cluster name cannot contain the delimiter.
The delimiter is ":" and the default namespace is "` + defaultClusterTagNamespace + `".

Tags of the following form will be added to the Instance in the appropriate
Cluster, as "<key>"="<value>":

    "<namespace>:<cluster-name>:<port>:<key>"="<value>"

If key/value tags are included, the cluster membership tag is optional.

Tags without the namespaced cluster/port prefix will be added to all Instances
in all Clusters to which the EC2 Instance belongs.

By default, all EC2 Instances in the VPC are examined, but additional filters
can be specified (see -filters).`
)

func AWSCmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	runner := &awsRunner{filterStrs: tbnflag.NewStrings()}

	cmd := &command.Cmd{
		Name:        "aws",
		Summary:     "aws collector",
		Usage:       "[OPTIONS]",
		Description: awsDescription,
		Runner:      runner,
	}

	flags := tbnflag.Wrap(&cmd.Flags)
	flags.StringVar(
		&runner.settings.namespace,
		"cluster-tag-namespace",
		defaultClusterTagNamespace,
		"The namespace for cluster tags",
	)

	flags.StringVar(
		&runner.settings.vpcID,
		"vpc-id",
		"",
		usage.Required("The ID of the VPC in which rotor is running"),
	)

	flags.Var(
		&runner.filterStrs,
		"filters",
		"A comma-delimited list of key/value pairs, used to specify additional "+
			"EC2 Instances filters. Of the form `\"<key>=<value>,...\"`. "+
			"See http://goo.gl/kSCOHS for a discussion of available filters.",
	)

	runner.awsFlags = newSessionFromFlags(flags)
	runner.updaterFlags = updaterFlags

	return cmd
}

type awsCollectorSettings struct {
	namespace string
	delimiter string
	vpcID     string
	filters   map[string][]string
}

type awsRunner struct {
	filterStrs   tbnflag.Strings
	settings     awsCollectorSettings
	awsFlags     sessionFromFlags
	updaterFlags rotor.UpdaterFromFlags
}

func (r *awsRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	filters, err := r.processFilters(r.filterStrs.Strings)
	if err != nil {
		return cmd.BadInput(err)
	}

	r.settings.filters = filters
	r.settings.delimiter = delimiter

	if err := r.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	u, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	c := awsCollector{
		settings: r.settings,
		ec2Svc:   ec2.New(r.awsFlags.Make()),
	}

	updater.Loop(u, c.getClusters)

	return command.NoError()
}

func (r *awsRunner) processFilters(strs []string) (map[string][]string, error) {
	filters := map[string][]string{}
	for _, str := range strs {
		key, value := tbnstrings.SplitFirstEqual(str)
		if key == "" || value == "" {
			return nil, fmt.Errorf("malformed filter: %q", str)
		}
		filters[key] = append(filters[key], value)
	}

	return filters, nil
}

type awsCollector struct {
	settings awsCollectorSettings
	ec2Svc   *ec2.EC2
}

func (c awsCollector) getClusters() ([]api.Cluster, error) {
	params := &ec2.DescribeInstancesInput{Filters: c.mkFilters()}
	resp, err := c.ec2Svc.DescribeInstances(params)
	if err != nil {
		return nil, fmt.Errorf("error executing aws api list: %s", err.Error())
	}
	return c.reservationsToClusters(resp.Reservations), nil
}

func (c awsCollector) mkFilters() []*ec2.Filter {
	filters := []*ec2.Filter{
		// return only running instances
		{
			Name:   aws.String("instance-state-name"),
			Values: []*string{aws.String("running")},
		},
		// in the provided VPC
		{
			Name:   aws.String("vpc-id"),
			Values: []*string{aws.String(c.settings.vpcID)},
		},
	}

	// add custom filters
	for key, values := range c.settings.filters {
		valuePtrs := []*string{}
		for _, value := range values {
			valuePtrs = append(valuePtrs, aws.String(value))
		}
		filters = append(filters, &ec2.Filter{Name: aws.String(key), Values: valuePtrs})
	}

	return filters
}

func (c awsCollector) reservationsToClusters(reservs []*ec2.Reservation) api.Clusters {
	clustersMap := map[string]*api.Cluster{}
	for _, res := range reservs {
		for _, inst := range res.Instances {
			c.processEC2Instance(clustersMap, inst)
		}
	}

	clusters := make(api.Clusters, 0, len(clustersMap))
	for _, cluster := range clustersMap {
		sort.Sort(api.InstancesByHostPort(cluster.Instances))
		clusters = append(clusters, *cluster)
	}
	sort.Sort(api.ClusterByName(clusters))

	return clusters
}

func (c awsCollector) processEC2Instance(clusters map[string]*api.Cluster, inst *ec2.Instance) {
	host := *inst.PrivateIpAddress
	tpm := tagAndPortMap{prefix: c.settings.namespace, delimiter: c.settings.delimiter}

	// process all tags, extracting cluster-namespaced key/value pairs and ports
	for _, tag := range inst.Tags {
		// TODO: consider adding other machine metadata as tags
		if err := tpm.processTag(*tag.Key, *tag.Value); err != nil {
			console.Error().Printf("Skipping tag for Instance %s: %s", host, err)
		}
	}

	for clusterAndPort, md := range tpm.clusterTagMap {
		metadata := api.MetadataFromMap(md)
		for key, value := range tpm.globalTagMap {
			metadata = append(metadata, api.Metadatum{Key: key, Value: value})
		}
		sort.Sort(api.MetadataByKey(metadata))

		instance := api.Instance{
			Host:     host,
			Port:     clusterAndPort.port,
			Metadata: metadata,
		}

		clusterName := clusterAndPort.cluster
		cluster := clusters[clusterName]
		if cluster == nil {
			cluster = &api.Cluster{
				Name:      clusterName,
				Instances: []api.Instance{},
			}
			clusters[clusterName] = cluster
		}

		cluster.Instances = append(cluster.Instances, instance)
	}
}

type clusterAndPort struct {
	cluster string
	port    int
}

func newClusterAndPort(terms []string) (clusterAndPort, error) {
	nope := clusterAndPort{}

	if len(terms) < 2 {
		return nope, errors.New("must have at least cluster and port")
	}

	port, err := strconv.ParseUint(terms[1], 10, 16)
	if err != nil {
		return nope, fmt.Errorf("bad port: %s", err)
	}
	if port == 0 {
		return nope, fmt.Errorf("port must be non zero")
	}

	if terms[0] == "" {
		return nope, errors.New("cluster must be non-empty")
	}

	return clusterAndPort{terms[0], int(port)}, nil
}

// encapsulates extracting tags and ports from prefixed keys and values for a
// single instance.
type tagAndPortMap struct {
	prefix        string
	delimiter     string
	clusterTagMap map[clusterAndPort]map[string]string
	globalTagMap  map[string]string
}

func (tpm *tagAndPortMap) processTag(key, value string) error {
	if key == "" {
		return fmt.Errorf("empty tag key for value: %q", value)
	}

	prefixWithDelim := tpm.prefix + tpm.delimiter

	// if it doesn't have the prefix, it's a global tag
	if !strings.HasPrefix(key, prefixWithDelim) {
		if tpm.globalTagMap == nil {
			tpm.globalTagMap = map[string]string{}
		}
		tpm.globalTagMap[key] = value
		return nil
	}

	// remove the prefix
	suffix := key[len(prefixWithDelim):]
	if suffix == "" {
		return fmt.Errorf("tag key empty after %q prefix removed: %q=%q", prefixWithDelim, key, value)
	}

	terms := strings.SplitN(suffix, tpm.delimiter, 3)
	if len(terms) < 2 {
		return fmt.Errorf("tag key must have at least cluster name and port: %q=%q", key, value)
	}

	candp, err := newClusterAndPort(terms)
	if err != nil {
		return fmt.Errorf("malformed cluster/port in tag key: %q: %s", suffix, err)
	}

	if tpm.clusterTagMap == nil {
		tpm.clusterTagMap = map[clusterAndPort]map[string]string{}
	}

	if tpm.clusterTagMap[candp] == nil {
		tpm.clusterTagMap[candp] = map[string]string{}
	}

	if len(terms) > 2 {
		k := terms[2]
		if k == "" {
			return fmt.Errorf("tag key cluster name and port, but empty key: %q", key)
		}

		tpm.clusterTagMap[candp][k] = value
	}

	return nil
}
