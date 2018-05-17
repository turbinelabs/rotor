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
	"sort"
	"testing"
	"time"

	ec2 "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	gomock "github.com/golang/mock/gomock"
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"
)

func TestECSCmd(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(nil)
	cmd := ECSCmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*ecsRunner)
	assert.NonNil(t, runner.awsFlags)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
}

func TestECSRunnerRun(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)
	mockUpdater.EXPECT().Delay().Return(time.Minute)
	mockUpdater.EXPECT().Close().Return(nil)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	sync := make(chan struct{}, 1)

	mockAWSClient := newMockAwsClient(ctrl)
	mockAWSClient.EXPECT().ListClusters().Return(map[string]arn{"cluster": "cluster"}, nil)
	mockAWSClient.EXPECT().
		ListServices("cluster").
		Return(nil, errors.New("boom")).
		Do(func(_ string) { sync <- struct{}{} })

	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockClientFromFlags.EXPECT().MakeAWSClient().Return(mockAWSClient)

	cmd := ECSCmd(mockUpdaterFromFlags)
	r := cmd.Runner.(*ecsRunner)
	r.awsFlags = mockClientFromFlags
	r.cfg.clusters.ResetDefault("cluster")

	result := make(chan command.CmdErr, 1)
	go func() {
		result <- r.Run(cmd, nil)
	}()

	<-sync

	updater.StopLoop()
	assert.Equal(t, <-result, command.NoError())
}

func TestECSRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	er := ecsRunner{updaterFlags: mockUpdaterFromFlags}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(err)

	cmdErr := er.Run(ECSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "ecs: "+err.Error())
}

func TestECSRunnerRunCfgValidateListClustersError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)

	mockAWS := newMockAwsClient(ctrl)
	mockAWS.EXPECT().ListClusters().Return(nil, errors.New("boom"))

	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockClientFromFlags.EXPECT().MakeAWSClient().Return(mockAWS)

	er := ecsRunner{
		awsFlags:     mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}

	cmdErr := er.Run(ECSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "ecs: boom")
}

func TestECSRunnerRunCfgValidateListClustersNotFound(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)

	mockAWS := newMockAwsClient(ctrl)
	mockAWS.EXPECT().ListClusters().Return(map[string]arn{"xyz": "pdq"}, nil)

	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockClientFromFlags.EXPECT().MakeAWSClient().Return(mockAWS)

	er := ecsRunner{
		cfg: ecsSettings{
			clusters: tbnflag.NewStrings(),
		},
		awsFlags:     mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}
	er.cfg.clusters.ResetDefault("c-is-for-cluster")

	cmdErr := er.Run(ECSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "ecs: ECS cluster c-is-for-cluster was not found")
}

func TestECSRunnerRunMakeUpdaterError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(nil, errors.New("no updater for you"))

	mockAWS := newMockAwsClient(ctrl)
	mockAWS.EXPECT().ListClusters().Return(map[string]arn{"cluster": "cluster-arn"}, nil)

	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockClientFromFlags.EXPECT().MakeAWSClient().Return(mockAWS)

	er := ecsRunner{
		cfg: ecsSettings{
			clusters: tbnflag.NewStrings(),
		},
		awsFlags:     mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}
	er.cfg.clusters.ResetDefault("cluster")

	cmdErr := er.Run(ECSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "ecs: no updater for you")
}

func TestECSGetClustersActionError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockAWS := newMockAwsClient(ctrl)
	mockAWS.EXPECT().ListServices("cluster").Return(nil, errors.New("boom"))

	ecsCfg := ecsSettings{
		clusters: tbnflag.NewStrings(),
	}
	ecsCfg.clusters.ResetDefault("cluster")

	clusters, err := ecsGetClustersAction(ecsCfg, mockAWS)
	assert.Nil(t, clusters)
	assert.ErrorContains(t, err, "boom")
}

func TestECSGetClustersAction(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockAWS := newMockAwsClient(ctrl)
	mockAWS.EXPECT().ListServices("cluster").Return(map[string]arn{"service": "service"}, nil)
	mockAWS.EXPECT().ServiceDefinitions("cluster", gomock.Any()).Return(
		map[arn]svcDefn{
			"service": {&ecs.Service{TaskDefinition: ptr.String("task")}},
		},
		nil,
	)
	mockAWS.EXPECT().TaskDefinition(arn("task")).Return(
		taskDefn{&ecs.TaskDefinition{
			ContainerDefinitions: []*ecs.ContainerDefinition{
				{
					Name: ptr.String("container"),
					DockerLabels: ptr.StringMap(map[string]string{
						"tbn-cluster": "cluster-1:1234",
					}),
					PortMappings: []*ecs.PortMapping{
						{ContainerPort: ptr.Int64(1234), HostPort: ptr.Int64(0)},
					},
				},
			},
		}},
		nil,
	)
	mockAWS.EXPECT().ListTasks("cluster", arn("service")).Return([]arn{"task"}, nil)
	mockAWS.EXPECT().GetTasks(gomock.Any(), "cluster", arn("task")).Return(
		map[arn]taskInst{
			"task": {
				&ecs.Task{
					TaskDefinitionArn:    ptr.String("task"),
					ContainerInstanceArn: ptr.String("container"),
					Containers: []*ecs.Container{
						{
							Name:         ptr.String("container"),
							ContainerArn: ptr.String("container"),
							NetworkBindings: []*ecs.NetworkBinding{
								{
									ContainerPort: ptr.Int64(1234),
									HostPort:      ptr.Int64(5678),
								},
							},
						},
					},
				},
			},
		},
		nil,
	)
	mockAWS.EXPECT().GetContainerInstances(gomock.Any(), "cluster", arn("container")).Return(
		map[arn]containerInst{
			"container": {&ecs.ContainerInstance{Ec2InstanceId: ptr.String("inst")}},
		},
		nil,
	)
	mockAWS.EXPECT().GetEC2Instances("inst").Return(
		map[string]ec2Instance{
			"inst": {&ec2.Instance{
				PrivateIpAddress: ptr.String("10.0.0.1")},
			},
		},
		nil,
	)

	ecsCfg := ecsSettings{
		clusters:   tbnflag.NewStrings(),
		clusterTag: "tbn-cluster",
	}
	ecsCfg.clusters.ResetDefault("cluster")

	clusters, err := ecsGetClustersAction(ecsCfg, mockAWS)

	sort.Sort(api.MetadataByKey(clusters[0].Instances[0].Metadata))

	assert.ArrayEqual(
		t,
		clusters,
		[]api.Cluster{
			{
				Name: "cluster-1",
				Instances: []api.Instance{
					{
						Host: "10.0.0.1",
						Port: 5678,
						Metadata: api.Metadata{
							api.Metadatum{Key: "ec2-instance-id", Value: "inst"},
							api.Metadatum{Key: "ecs-cluster", Value: "cluster"},
							api.Metadatum{Key: "ecs-container-instance", Value: "container"},
							api.Metadatum{Key: "ecs-service", Value: ""},
							api.Metadatum{Key: "ecs-service-arn", Value: "service"},
							api.Metadatum{Key: "ecs-task-container", Value: "container"},
							api.Metadatum{Key: "ecs-task-definition", Value: "task"},
							api.Metadatum{Key: "ecs-task-instance", Value: "task"},
						},
					},
				},
			},
		},
	)
	assert.Nil(t, err)
}
