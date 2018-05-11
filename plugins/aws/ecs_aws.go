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

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"

	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/nonstdlib/ptr"
)

const (
	DescribeServicesWindowSz = 10
	DescribeTasksWindowSz    = 100
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type awsClient,ecsInterface,ec2Interface -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

type arn string

// awsClient represents an adapter than handles making calls to AWS.
type awsClient interface {
	// ListClusters returns all clusters visible to ECS collector. It returns them
	// mapped to their full ARN. An error is returned if encountered retrieving
	// clusters from ECS.
	ListClusters() (map[string]arn, error)

	// ListServices returns all services created within a specified cluster. They
	// are returned as a map from their short name to the full ARN. If an error
	// is encountered while producing this list it is returned.
	ListServices(cluster string) (map[string]arn, error)

	// ListTasks looks through a specified cluster for running tasks. If a service
	// name is specified only tasks running that service will be returned. The
	// task identifier returned is an ARN.
	ListTasks(cluster string, svc arn) ([]arn, error)

	// ServiceDefinitions takes a given cluster and set of service ARNs returning
	// a mapping from ARN to the definition of that service.
	ServiceDefinitions(cluster string, svcARN []arn) (map[arn]svcDefn, error)

	// TaskDefinition gets the definition of a task specificed by ARN.
	TaskDefinition(taskARN arn) (taskDefn, error)

	// GetTasks loads descrptions of running Tasks (from ARN to task metadata).
	// If a task ARN has already been looked up (determined by checking for its
	// presence in the hasTasks map) then it will not be requested.
	GetTasks(
		hasTasks map[arn]taskInst,
		cluster string,
		taskARN ...arn,
	) (map[arn]taskInst, error)

	// GetContainerInstances loads descrptions of live ContainerInstances into a
	// map (from ARN to descrption). If a task ARN has already been looked up
	// (determined by checking for its presence in the hasInst map) then it is
	// removed from the ARNs requested.
	GetContainerInstances(
		hasContainers map[arn]containerInst,
		cluster string,
		ciarn ...arn,
	) (map[arn]containerInst, error)

	// GetEC2Instances returns running instances based on their instance id.
	GetEC2Instances(instIDs ...string) (map[string]ec2Instance, error)
}

// ecsInterface is an interface that allows us to mock an ECS client, see
// github.com/aws/aws-sdk-go/service/ecs/api.go for method docs.
type ecsInterface interface {
	ListClusters(*ecs.ListClustersInput) (*ecs.ListClustersOutput, error)
	ListServices(*ecs.ListServicesInput) (*ecs.ListServicesOutput, error)
	ListTasks(*ecs.ListTasksInput) (*ecs.ListTasksOutput, error)
	DescribeServices(*ecs.DescribeServicesInput) (*ecs.DescribeServicesOutput, error)
	DescribeTaskDefinition(
		*ecs.DescribeTaskDefinitionInput) (*ecs.DescribeTaskDefinitionOutput, error)
	DescribeTasks(*ecs.DescribeTasksInput) (*ecs.DescribeTasksOutput, error)
	DescribeContainerInstances(
		*ecs.DescribeContainerInstancesInput) (*ecs.DescribeContainerInstancesOutput, error)
}

// ec2Interface is an interface that allows us to mock an EC2 client see
// github.com/aws/aws-sdk-go/service/ec2/api.go for method docs.
type ec2Interface interface {
	DescribeInstances(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
}

type awsAdapter struct {
	ecs ecsInterface
	ec2 ec2Interface

	describeServicesWindowSz int
	describeTasksWindowSz    int
}

var _ awsClient = awsAdapter{}

func newAwsClient(ecs ecsInterface, ec2 ec2Interface) awsClient {
	return awsAdapter{ecs, ec2, DescribeServicesWindowSz, DescribeTasksWindowSz}
}

var badWindowSize = errors.New("invalid window size")

func (a awsAdapter) ListClusters() (map[string]arn, error) {
	arg := &ecs.ListClustersInput{}
	ecsClusters := map[string]arn{}

	for {
		out, err := a.ecs.ListClusters(arg)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve ECS clusters: %s", err.Error())
		}

		extractSNARNMap("clusters", ecsClusters, out.ClusterArns)
		arg.NextToken = out.NextToken
		if arg.NextToken == nil {
			break
		}
	}

	return ecsClusters, nil
}

func (a awsAdapter) ListServices(cluster string) (map[string]arn, error) {
	results := map[string]arn{}
	arg := &ecs.ListServicesInput{}
	arg.Cluster = &cluster

	moreServices := true
	for moreServices {
		out, err := a.ecs.ListServices(arg)
		if err != nil {
			return nil, fmt.Errorf(
				"could not list services for cluster %s: %s", cluster, err.Error())
		}

		extractSNARNMap("services", results, out.ServiceArns)
		arg.NextToken = out.NextToken
		moreServices = arg.NextToken != nil
	}

	return results, nil
}

func (a awsAdapter) ListTasks(cluster string, svc arn) ([]arn, error) {
	arg := &ecs.ListTasksInput{Cluster: &cluster}
	if svc != "" {
		arg.ServiceName = ptr.String(string(svc))
	}

	svcTasks := []arn{}

	moreTasks := true
	for moreTasks {
		out, err := a.ecs.ListTasks(arg)
		if err != nil {
			return nil, err
		}

		for _, sptr := range out.TaskArns {
			svcTasks = append(svcTasks, arnValue(sptr))
		}

		arg.NextToken = out.NextToken
		moreTasks = arg.NextToken != nil
	}

	return svcTasks, nil
}

func min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

func sliceWalk(windowSz int, input []*string, fn func([]*string) error) error {
	if windowSz < 1 {
		return badWindowSize
	}

	cnt := len(input)
	for i := 0; i < cnt; i += windowSz {
		err := fn(input[i : i+min(windowSz, cnt-i)])
		if err != nil {
			return err
		}
	}
	return nil
}

func (a awsAdapter) ServiceDefinitions(
	cluster string,
	svcARN []arn,
) (map[arn]svcDefn, error) {
	svcs := ptr.StringSlice(a2s(svcARN))
	result := map[arn]svcDefn{}

	err := sliceWalk(a.describeServicesWindowSz, svcs, func(ids []*string) error {
		arg := &ecs.DescribeServicesInput{Cluster: &cluster, Services: ids}

		out, err := a.ecs.DescribeServices(arg)
		if err != nil {
			return fmt.Errorf("failed to describe %s services: %s", cluster, err.Error())
		}

		for _, f := range out.Failures {
			console.Error().Printf("%v: %v", ptr.StringValue(f.Arn), ptr.StringValue(f.Reason))
		}

		for _, s := range out.Services {
			result[arnValue(s.ServiceArn)] = svcDefn{s}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

func (a awsAdapter) TaskDefinition(taskARN arn) (taskDefn, error) {
	arg := &ecs.DescribeTaskDefinitionInput{TaskDefinition: ptr.String(string(taskARN))}
	out, err := a.ecs.DescribeTaskDefinition(arg)
	if err != nil {
		return taskDefn{}, fmt.Errorf(
			"unable to load task definition for %s: %s", taskARN, err.Error())
	}

	return taskDefn{out.TaskDefinition}, nil
}

func (a awsAdapter) GetTasks(
	hasTasks map[arn]taskInst,
	cluster string,
	taskARN ...arn,
) (map[arn]taskInst, error) {
	dest := map[arn]taskInst{}
	argIDs := []string{}
	for _, tid := range taskARN {
		if _, ok := hasTasks[tid]; !ok {
			argIDs = append(argIDs, string(tid))
		}
	}
	if len(argIDs) == 0 {
		return nil, nil
	}

	taskIDs := ptr.StringSlice(argIDs)
	err := sliceWalk(a.describeTasksWindowSz, taskIDs, func(ids []*string) error {
		arg := &ecs.DescribeTasksInput{Tasks: ids}
		if cluster != "" {
			arg.Cluster = &cluster
		}

		out, err := a.ecs.DescribeTasks(arg)
		if err != nil {
			return fmt.Errorf("failed to load task instance data: %s", err.Error())
		}

		for _, f := range out.Failures {
			console.Error().Printf("%s: %s", ptr.StringValue(f.Arn), ptr.StringValue(f.Reason))
		}

		for _, t := range out.Tasks {
			id := arnValue(t.TaskArn)
			dest[id] = taskInst{t}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return dest, nil
}

func (a awsAdapter) GetContainerInstances(
	hasInst map[arn]containerInst,
	cluster string,
	ciarn ...arn,
) (map[arn]containerInst, error) {
	argIds := []string{}
	for _, id := range ciarn {
		if _, ok := hasInst[id]; !ok {
			argIds = append(argIds, string(id))
		}
	}
	if len(argIds) == 0 {
		return nil, nil
	}

	dest := map[arn]containerInst{}

	arg := &ecs.DescribeContainerInstancesInput{
		Cluster: &cluster, ContainerInstances: ptr.StringSlice(argIds)}
	out, err := a.ecs.DescribeContainerInstances(arg)
	if err != nil {
		return nil, fmt.Errorf("unable to load container instance data: %s", err.Error())
	}

	for _, f := range out.Failures {
		console.Error().Printf("%s: %s", ptr.StringValue(f.Arn), ptr.StringValue(f.Reason))
	}

	for _, ci := range out.ContainerInstances {
		id := arnValue(ci.ContainerInstanceArn)
		dest[id] = containerInst{ci}
	}

	return dest, nil
}

func (a awsAdapter) GetEC2Instances(instIDs ...string) (map[string]ec2Instance, error) {
	if len(instIDs) == 0 {
		return nil, nil
	}

	result := map[string]ec2Instance{}
	arg := &ec2.DescribeInstancesInput{InstanceIds: ptr.StringSlice(instIDs)}

	moreInstances := true
	for moreInstances {
		out, err := a.ec2.DescribeInstances(arg)
		if err != nil {
			return nil, fmt.Errorf("unable to load EC2 instance data: %s", err.Error())
		}

		for _, reservation := range out.Reservations {
			for _, inst := range reservation.Instances {
				iid := ptr.StringValue(inst.InstanceId)
				if _, ok := result[iid]; !ok && inst != nil && iid != "" {
					result[iid] = ec2Instance{inst}
				}
			}
		}

		arg.NextToken = out.NextToken
		moreInstances = arg.NextToken != nil
	}

	return result, nil
}

func a2s(a []arn) []string {
	s := make([]string, 0, len(a))
	for _, e := range a {
		s = append(s, string(e))
	}
	return s
}

func s2a(s []string) []arn {
	a := make([]arn, 0, len(s))
	for _, e := range s {
		a = append(a, arn(e))
	}
	return a
}
