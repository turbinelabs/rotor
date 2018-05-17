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

package marathon

//go:generate mockgen -destination mock_marathon_client_test.go --write_package_comment=false -package $GOPACKAGE github.com/gambol99/go-marathon Marathon

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"

	marathon "github.com/gambol99/go-marathon"
)

var (
	clusters  map[string]*api.Cluster
	collector marathonCollector
	ls        map[string]string
	app       marathon.Application
	tasks     marathon.Tasks
	group     marathon.Group
	groups    marathon.Groups
	fla       = float64(12.3)
	flb       = float64(1776.0)
)

const (
	parentGroupID    = "/a"
	groupID          = parentGroupID + "/b"
	appID            = groupID + "/c"
	taskID           = appID + "/d-1234-5678"
	host             = "1.2.3.4"
	port             = 56789
	ver              = "idk"
	clusterLabelName = "where_to"
)

func resetMarathonFixtures() {
	clusters = map[string]*api.Cluster{}
	collector = marathonCollector{
		marathonCollectorSettings: marathonCollectorSettings{
			clusterLabelName: clusterLabelName,
			groupPrefix:      "/a/b",
		},
	}
	tasks = marathon.Tasks{
		Tasks: []marathon.Task{
			{
				ID:      taskID,
				AppID:   appID,
				Host:    host,
				Ports:   []int{port},
				Version: ver,
			}}}
	app = marathon.Application{
		ID:      appID,
		Version: ver,
		CPUs:    80.286,
		Mem:     &fla,
		Disk:    &flb,
		Labels:  &ls,
	}
	group = marathon.Group{
		ID: parentGroupID,
		Groups: []*marathon.Group{
			{
				ID:   groupID,
				Apps: []*marathon.Application{&app},
			},
		},
	}
	groups = marathon.Groups{
		Groups: []*marathon.Group{&group},
	}
	ls = map[string]string{
		"name":     "bozo",
		"where_to": "outer_space",
	}
}

func TestCmd(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*marathonRunner)
	assert.NonNil(t, runner.updaterFlags)
	assert.NonNil(t, runner.clientFlags)
}

func TestMarathonRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockClientFromFlags := newMockClientFromFlags(ctrl)

	mr := marathonRunner{
		clientFlags:  mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}
	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(err)

	cmdErr := mr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "marathon: "+err.Error())
}

func TestMarathonRunnerRunBadClientFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockClientFromFlags := newMockClientFromFlags(ctrl)

	mr := marathonRunner{
		clientFlags:  mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockClientFromFlags.EXPECT().Validate().Return(err)

	cmdErr := mr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "marathon: "+err.Error())
}

func TestMarathonRunnerRunMakeUpdaterError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockClientFromFlags := newMockClientFromFlags(ctrl)

	mr := marathonRunner{
		clientFlags:  mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockClientFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(nil, err)

	cmdErr := mr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "marathon: "+err.Error())
}

func TestMarathonRunnerRunMakeMarathonClientError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockUpdater := updater.NewMockUpdater(ctrl)

	mr := marathonRunner{
		clientFlags:  mockClientFromFlags,
		updaterFlags: mockUpdaterFromFlags,
	}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockClientFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)
	mockClientFromFlags.EXPECT().Make().Return(nil, err)

	cmdErr := mr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "marathon: Unable to instantiate Marathon client: boom")
}

func TestMarathonRunnerRunBadLabelSelector(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockClientFromFlags := newMockClientFromFlags(ctrl)

	mr := marathonRunner{
		marathonCollectorSettings: marathonCollectorSettings{selector: "=nope"},
		clientFlags:               mockClientFromFlags,
		updaterFlags:              mockUpdaterFromFlags,
	}

	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockClientFromFlags.EXPECT().Validate().Return(nil)

	cmdErr := mr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.HasPrefix(t, cmdErr.Message, "marathon: Error parsing selector: ")
}

func TestMarathonMakeFilter(t *testing.T) {
	mr := marathonRunner{
		marathonCollectorSettings: marathonCollectorSettings{
			selector: "yeah=ok",
		},
	}
	filter, err := mr.makeFilter()
	assert.Nil(t, err)
	assert.NonNil(t, filter)
}

func TestMarathonFilters(t *testing.T) {
	resetMarathonFixtures()
	var err error
	f1, err := makeLabelFilter("name=bozo")
	assert.Nil(t, err)
	f2, err := makeLabelFilter("crazy=certainly")
	assert.Nil(t, err)
	var filters = []marathonFilter{f1, f2}
	fa := makeFilterStack(filters)
	assert.False(t, fa(nil, nil, nil, ls))
	ls["crazy"] = "certainly"
	assert.True(t, fa(nil, nil, nil, ls))
}

func TestMarathonGetClustersNormal(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()

	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(&groups, nil)
	mockClient.EXPECT().Tasks(appID).Return(&tasks, nil)

	collector.client = mockClient
	clusters, err := collector.getClusters()

	assert.Nil(t, err)
	assert.ArrayEqual(
		t,
		clusters, api.Clusters{
			{
				Name: "outer_space",
				Instances: []api.Instance{
					{
						Host: host,
						Port: port,
						Metadata: api.Metadata{
							api.Metadatum{Key: "app/id", Value: "/a/b/c"},
							api.Metadatum{Key: "app/required_cpus", Value: "80.286"},
							api.Metadatum{Key: "app/required_disk", Value: "1776.000"},
							api.Metadatum{Key: "app/required_mem", Value: "12.300"},
							api.Metadatum{Key: "app/version", Value: ver},
							api.Metadatum{Key: "name", Value: "bozo"},
						},
					},
				},
			},
		},
	)
}

func TestMarathonGetClustersGroupError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()
	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(nil, errors.New("boom"))

	collector.client = mockClient
	clusters, err := collector.getClusters()
	assert.ErrorContains(t, err, "boom")
	assert.Nil(t, clusters)
}

func TestMarathonGetClustersNoTasks(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()
	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(&groups, nil)
	mockClient.EXPECT().Tasks(appID).Return(&marathon.Tasks{Tasks: []marathon.Task{}}, nil)

	collector.client = mockClient
	clusters, err := collector.getClusters()
	assert.Nil(t, err)
	assert.ArrayEqual(
		t,
		clusters,
		api.Clusters{{Name: "outer_space"}},
	)
}

func TestMarathonGetClustersFetchError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()
	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(&groups, nil)
	mockClient.EXPECT().Tasks(appID).Return(nil, errors.New("boom"))

	collector.client = mockClient
	clusters, err := collector.getClusters()
	assert.ErrorContains(t, err, "no clusters found")
	assert.Nil(t, clusters)
}

func TestMarathonGetClustersNoMatchingGroups(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()
	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(&groups, nil)

	collector.groupPrefix = "/z"
	collector.client = mockClient
	clusters, err := collector.getClusters()
	assert.ErrorContains(t, err, "no clusters found")
	assert.Nil(t, clusters)
}

func TestMarathonGetClustersNoMatchingNestedGroups(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	resetMarathonFixtures()
	mockClient := NewMockMarathon(ctrl)
	mockClient.EXPECT().Groups().Return(&groups, nil)

	collector.groupPrefix = "/a/z"
	collector.client = mockClient
	clusters, err := collector.getClusters()
	assert.ErrorContains(t, err, "no clusters found")
	assert.Nil(t, clusters)
}

func TestMarathonLabelsForApp(t *testing.T) {
	resetMarathonFixtures()
	lsb := labelsForApp(&app)
	assert.Equal(t, lsb[marathonAppPrefix+"id"], app.ID)
	assert.Equal(t, lsb[marathonAppPrefix+"version"], app.Version)
	assert.Equal(t, lsb[marathonAppPrefix+"required_cpus"], "80.286")
	assert.Equal(t, lsb[marathonAppPrefix+"required_mem"], "12.300")
	assert.Equal(t, lsb[marathonAppPrefix+"required_disk"], "1776.000")
}

func TestMarathonHandleAppNormal(t *testing.T) {
	resetMarathonFixtures()
	err := collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
	cluster := clusters["outer_space"]
	assert.NonNil(t, cluster)
	assert.Equal(t, cluster.Name, "outer_space")
	assert.Equal(t, len(cluster.Instances), 1)
	md := cluster.Instances[0].Metadata.Map()
	assert.Equal(t, md["app/id"], appID)
	assert.Equal(t, md["app/version"], app.Version)
}

func TestMarathonHandleAppHandleDupes(t *testing.T) {
	resetMarathonFixtures()
	err := collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
	err = collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
	cluster := clusters["outer_space"]
	assert.NonNil(t, cluster)
	assert.Equal(t, cluster.Name, "outer_space")
	assert.Equal(t, len(cluster.Instances), 2)
	md := cluster.Instances[0].Metadata.Map()
	assert.Equal(t, md["app/id"], appID)
	assert.Equal(t, md["app/version"], app.Version)
}

func TestMarathonHandleAppFiltered(t *testing.T) {
	resetMarathonFixtures()
	filter, err := makeLabelFilter("x=y")
	assert.Nil(t, err)
	collector.filter = filter
	err = collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
	cluster := clusters["outer_space"]
	assert.NonNil(t, cluster)
	assert.Equal(t, cluster.Name, "outer_space")
	assert.Equal(t, len(cluster.Instances), 0)
	collector.filter = nil
}

func TestMarathonHandleAppMissingClusterKey(t *testing.T) {
	resetMarathonFixtures()
	delete(ls, collector.clusterLabelName)
	err := collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
}

func TestMarathonHandleAppMissingPort(t *testing.T) {
	resetMarathonFixtures()
	tasks.Tasks[0].Ports = nil
	err := collector.handleApp(clusters, &app, &tasks)
	assert.Nil(t, err)
	cluster := clusters["outer_space"]
	assert.NonNil(t, cluster)
	assert.Equal(t, len(cluster.Instances), 0)
}
