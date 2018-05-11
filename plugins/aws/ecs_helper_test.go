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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/matcher"
)

func testParseLabel(
	t *testing.T,
	in string,
	wantSvcs []string,
	wantPorts []int,
	wantErr error,
) {
	svcs, ports, err := parseLabel(in)
	assert.DeepEqual(t, svcs, wantSvcs)
	assert.DeepEqual(t, ports, wantPorts)
	if wantErr == nil {
		assert.Nil(t, err)
	} else {
		assert.ErrorContains(t, err, wantErr.Error())
	}
}

func TestParseLabel(t *testing.T) {
	testParseLabel(t, "", nil, nil, errors.New("'' must be in the format service:port"))
	testParseLabel(t, "svc:port", nil, nil, errors.New("'svc:port' has an invalid port binding"))
	testParseLabel(t, "svc:80", []string{"svc"}, []int{80}, nil)
	testParseLabel(
		t,
		"svc1:80,svc2:80,svc3:8080",
		[]string{"svc1", "svc2", "svc3"},
		[]int{80, 80, 8080},
		nil,
	)
	testParseLabel(
		t, "svc1:80,:50,svc3:50", nil, nil, errors.New("':50' contains no service declaration"))
	testParseLabel(
		t, "svc1:80,svc:,svc3:50", nil, nil, errors.New("'svc:' has an invalid port binding"))
	testParseLabel(
		t, "svc1:80,,svc3:50", nil, nil, errors.New("'' must be in the format service:port"))
}

func TestPortMappings(t *testing.T) {
	pm := portMappings{
		{80, 8000},
		{80, 8001},
		{80, 8002},
		{8080, 8080},
	}

	assert.DeepEqual(t, pm.mapsTo(80), []int{8000, 8001, 8002})
	assert.DeepEqual(t, pm.mapsTo(8080), []int{8080})
	assert.DeepEqual(t, pm.mapsTo(8000), []int{})
}

var (
	randStr1 = randStr(10)
	randStr2 = randStr(10)
	cluster1 = "cluster1"
	c1arn    = arn("arn:cluster/cluster1")
	cluster2 = "cluster2"
	c2arn    = arn("arn:cluster/cluster2")

	c1s1arn = arn("arn:cluster1/service1")
	c1s2arn = arn("arn:cluster1/service2")
	c2s1arn = arn("arn:cluster2/service1")
	c2s2arn = arn("arn:cluster2/service2")

	c1s1name = "c1service-1-name"
	c1s2name = "c1service-2-name"
	c1c1name = "cluster-container-1"
	c1c2name = "cluster-container-2"

	c2s1name = "c2service-1-name"
	c2s2name = "c2service-2-name"
	task1arn = arn("arn:aws/task1")
	task2arn = arn("arn:aws/task2")

	c1s1def = svcDefn{&ecs.Service{
		ClusterArn:     ptr.String(string(c1arn)),
		ServiceArn:     ptr.String(string(c1s1arn)),
		ServiceName:    &c1s1name,
		TaskDefinition: ptr.String(string(task1arn)),
	}}
	c1s2def = svcDefn{&ecs.Service{
		ClusterArn:     ptr.String(string(c1arn)),
		ServiceArn:     ptr.String(string(c1s2arn)),
		ServiceName:    &c1s2name,
		TaskDefinition: ptr.String(string(task2arn)),
	}}
	//c2s1def     = svcDefn{&ecs.Service{}}
	//c2s2def     = svcDefn{&ecs.Service{}}

	tag         = "tbn-cluster"
	apiSvc1     = "api-svc-1"
	apiSvc1Port = 80
	apiSvc2     = "api-svc-2"
	apiSvc2Port = 8080

	t1c1name = "task1-container1"
	t1Def    = &ecs.TaskDefinition{
		TaskDefinitionArn: ptr.String(string(task1arn)),
		ContainerDefinitions: []*ecs.ContainerDefinition{
			{
				Name: &c1c1name,
				DockerLabels: ptr.StringMap(map[string]string{
					tag:     fmt.Sprintf("%s:%d,%s:%d", apiSvc1, apiSvc1Port, apiSvc2, apiSvc2Port),
					"other": randStr1,
				}),
				PortMappings: []*ecs.PortMapping{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(0)},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(0)},
				},
			},
		},
	}

	t2Def = &ecs.TaskDefinition{
		TaskDefinitionArn: ptr.String(string(task2arn)),
		ContainerDefinitions: []*ecs.ContainerDefinition{
			{
				Name: &c1c2name,
				DockerLabels: ptr.StringMap(map[string]string{
					tag:     fmt.Sprintf("%s:%v", apiSvc1, apiSvc1Port),
					"other": randStr2,
					"stage": "prod",
				}),
				PortMappings: []*ecs.PortMapping{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(4545)},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(0)},
				},
			},
		},
	}

	testECSMeta = ecsMeta{
		clusters: []string{cluster1, cluster2},
		clusterSvcs: map[string][]arn{
			cluster1: {c1s1arn, c1s2arn},
			//cluster2: []string{c2s1arn, c2s2arn},
		},

		services: map[string]map[arn]svcDefn{
			cluster1: {
				c1s1arn: c1s1def,
				c1s2arn: c1s2def,
			},
			//cluster2: map[string]svcDefn{c2s1arn: c2s1def, c2s2arn: c2s2def},
		},

		tasks: map[arn]taskDefn{
			task1arn: {t1Def},
			task2arn: {t2Def},
		},
	}

	c1s1_task1arn = arn("arn:some-task-c1svc1/task-instance-1")
	c1s1_task2arn = arn("arn:some-task-c1svc1/task-instance-2")
	c1s2_task1arn = arn("arn:some-task-c1svc2/task-instance-1")

	cinst1arn = arn("arn:container-instance/inst1")
	cinst2arn = arn("arn:container-instance/inst2")
	cinst3arn = arn("arn:container-instance/inst3")

	ti1port1 = 23482
	ti1port2 = 31827
	ti2port1 = 23582
	ti2port2 = 31927
	ti3port1 = 4545
	ti3port2 = 8085

	host1arn = "arn:aws:ec2/host1"
	host2arn = "arn:aws:ec2/host2"
	host1Ip  = "10.0.1.15"
	host2Ip  = "10.0.1.16"

	c1s1ti1_containerarn = arn("c1s1ti1_containerarn/container")
	c1s1ti2_containerarn = arn("c1s1ti2_containerarn/container")
	c1s2ti1_containerarn = arn("c1s2ti1_containerarn/container")

	c1s1t1inst = taskInst{&ecs.Task{
		TaskArn:              ptr.String(string(c1s1_task1arn)),
		TaskDefinitionArn:    ptr.String(string(task1arn)),
		ClusterArn:           &cluster1,
		ContainerInstanceArn: ptr.String(string(cinst1arn)),
		Containers: []*ecs.Container{
			{
				ContainerArn: ptr.String(string(c1s1ti1_containerarn)),
				Name:         &c1c1name, NetworkBindings: []*ecs.NetworkBinding{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(int64(ti1port1))},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(int64(ti1port2))},
				},
			},
		},
	}}

	c1s1t2inst = taskInst{&ecs.Task{
		TaskArn:              ptr.String(string(c1s1_task1arn)),
		TaskDefinitionArn:    ptr.String(string(task1arn)),
		ClusterArn:           &cluster1,
		ContainerInstanceArn: ptr.String(string(cinst2arn)),
		Containers: []*ecs.Container{
			{
				ContainerArn: ptr.String(string(c1s1ti2_containerarn)),
				Name:         &c1c1name, NetworkBindings: []*ecs.NetworkBinding{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(int64(ti2port1))},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(int64(ti2port2))},
				},
			},
		},
	}}

	c1s2t1inst = taskInst{&ecs.Task{
		TaskArn:              ptr.String(string(c1s2_task1arn)),
		TaskDefinitionArn:    ptr.String(string(task2arn)),
		ClusterArn:           &cluster1,
		ContainerInstanceArn: ptr.String(string(cinst1arn)),
		Containers: []*ecs.Container{
			{
				ContainerArn: ptr.String(string(c1s2ti1_containerarn)),
				Name:         &c1c2name, NetworkBindings: []*ecs.NetworkBinding{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(int64(ti3port1))},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(int64(ti3port2))},
				},
			},
		},
	}}

	testECSRunning = ecsRunning{
		svcTasks: map[string]map[arn][]arn{
			cluster1: {
				c1s1arn: {c1s1_task1arn, c1s1_task2arn},
				c1s2arn: {c1s2_task1arn},
			},
		},

		taskInstances: map[arn]taskInst{
			c1s1_task1arn: c1s1t1inst,
			c1s1_task2arn: c1s1t2inst,
			c1s2_task1arn: c1s2t1inst,
		},

		containerInstances: map[arn]containerInst{
			cinst1arn: {&ecs.ContainerInstance{
				ContainerInstanceArn: ptr.String(string(cinst1arn)),
				Ec2InstanceId:        &host1arn,
			}},
			cinst2arn: {&ecs.ContainerInstance{
				ContainerInstanceArn: ptr.String(string(cinst2arn)),
				Ec2InstanceId:        &host2arn,
			}},
		},

		ec2Hosts: map[string]ec2Instance{
			host1arn: {&ec2.Instance{
				InstanceId:       &host1arn,
				PrivateIpAddress: &host1Ip,
			}},
			host2arn: {&ec2.Instance{
				InstanceId:       &host2arn,
				PrivateIpAddress: &host2Ip,
			}},
		},
	}

	testECSState = ecsState{testECSMeta, testECSRunning}
)

/*

The above constructs the following as a fixture

clusters:
	cluster1 (cluster1, c1arn)
	cluster2 (cluster2, c1arn)

cluster1 services:
	arn:cluster1/service1 (c1s1arn)
	arn:cluster1/service2 (c1s2arn)

cluster2 services:
	arn:cluster2/service1 (c2s1arn)
	arn:cluster2/service2 (c2s2arn)

cluster1/service1: (c1s1def)
	name: c1service-1-name (c1s1name)
	task: arn:aws/task1 (task1arn)

cluster1/service2: (c1s2def)
	name: c1service-2-name (c1s2name)
	task: arn:aws/task2 (task2arn)

tasks:
	arn:aws/task1 (task1arn, t1Def)
		container: cluster-container-1 (c1c1name)
		labels:
			tbn-cluster: api-svc1:80,api-svc-2:8080
			other: <randStr-10>
		ports:
			80 -> 0
			8080 -> 0

	arn:aws/task2 (task2arn, t2Def)
		container: cluster-container-2 (c1c2name)
		labels:
			tbn-cluster: api-svc1:80
			other: <randStr-10>
		ports:
			80 -> 4545
			8080 -> 0

task instances:
	instance 1: (c1s1_task1arn)
		Definition: task1arn
			(carries tag for api-svc-1, api-svc-2)
		Cluster: cluster1
		ContainerInstance: cinst1arn
		Containers:
			c1s1ti1_containerarn
			c1c1name
				80 -> 23482 (ti1port1)
				8080 -> 31827 (ti1port2)

	instance 2: (c1s1_task2arn)
		Definition: task1arn
			(carries tag for api-svc-1, api-svc-2)
		Cluster: cluster1
		ContainerInstance: cinst2arn
		Containers:
			c1s1ti2_containerarn
			c1c1name
				80 -> 23582 (ti2port1)
				8080 -> 31927 (t22port2)

	instance 3: (c1s2_task1arn)
		Definition: task2arn
			(carries tag for api-svc-1)
		Cluster: cluster2
		ContainerInstance: cinst1arn
		Containers:
			c1s2ti1_containerarn
			c1c2name
				80 -> 4545 (ti3port1)
				8080 -> 8085 (ti3port2)

container instances:
	cinst1arn
		on host1arn (host1Ip)
	cinst2arn
		on host2arn (host2Ip)
*/

func TestIdentifyTaggedItems(t *testing.T) {
	cfg := ecsSettings{clusterTag: tag}
	expected := []containerBindTemplate{
		{
			cfg,
			cluster1,
			c1s1arn,
			task1arn,
			c1c1name,
			[]string{apiSvc1, apiSvc2},
			[]int{apiSvc1Port, apiSvc2Port},
			t1Def.ContainerDefinitions[0].DockerLabels,
		},
		{
			cfg,
			cluster1,
			c1s2arn,
			task2arn,
			c1c2name,
			[]string{apiSvc1},
			[]int{apiSvc1Port},
			t2Def.ContainerDefinitions[0].DockerLabels,
		},
	}

	// 1. happy path
	cbt := testECSMeta.identifyTaggedItems(cfg)
	assert.HasSameElements(t, cbt, expected)

	initialSvcs := testECSMeta.clusterSvcs[cluster1]

	// 2. missing service definition
	newSvcs := testECSMeta.clusterSvcs[cluster1]
	testSvcArn := arn(randStr(10))
	testTaskArn := arn(randStr(10))

	newSvcs = append(newSvcs, testSvcArn)
	testECSMeta.clusterSvcs[cluster1] = newSvcs

	cbt = testECSMeta.identifyTaggedItems(cfg)
	assert.HasSameElements(t, cbt, expected)

	// 3. unset task definition
	testECSMeta.services[cluster1][testSvcArn] = svcDefn{&ecs.Service{
		ClusterArn:     ptr.String(string(c1arn)),
		ServiceArn:     ptr.String(string(testSvcArn)),
		ServiceName:    ptr.String(randStr(10)),
		TaskDefinition: ptr.String(""),
	}}

	cbt = testECSMeta.identifyTaggedItems(cfg)
	assert.HasSameElements(t, cbt, expected)

	// 4. missing task definition
	testECSMeta.services[cluster1][testSvcArn].TaskDefinition = ptr.String(string(testTaskArn))
	cbt = testECSMeta.identifyTaggedItems(cfg)
	assert.HasSameElements(t, cbt, expected)

	// 5. bad label
	testECSMeta.tasks[testTaskArn] = taskDefn{&ecs.TaskDefinition{
		TaskDefinitionArn: ptr.String(string(task1arn)),
		ContainerDefinitions: []*ecs.ContainerDefinition{
			{
				Name: &c1c1name,
				DockerLabels: ptr.StringMap(map[string]string{
					tag:     fmt.Sprintf("%s%d,%s:%d", apiSvc1, apiSvc1Port, apiSvc2, apiSvc2Port),
					"other": randStr(10),
				}),
				PortMappings: []*ecs.PortMapping{
					{ContainerPort: ptr.Int64(int64(apiSvc1Port)), HostPort: ptr.Int64(0)},
					{ContainerPort: ptr.Int64(int64(apiSvc2Port)), HostPort: ptr.Int64(0)},
				},
			},
		},
	}}
	cbt = testECSMeta.identifyTaggedItems(cfg)
	assert.HasSameElements(t, cbt, expected)

	// reset testECSMeta to something expected / good
	testECSMeta.clusterSvcs[cluster1] = initialSvcs
	delete(testECSMeta.services[cluster1], testSvcArn)
	delete(testECSMeta.tasks, testTaskArn)
}

func TestStateValidate(t *testing.T) {
	base := func() containerBindTemplate {
		return containerBindTemplate{
			cfg:       ecsSettings{},
			cluster:   cluster1,
			service:   c1s1arn,
			task:      task1arn,
			container: c1c1name,
			svcs:      []string{apiSvc1},
			ports:     []int{apiSvc1Port},
			labels:    map[string]*string{},
		}
	}

	cbt := base()
	gotErr := testECSState.validate(cbt)
	assert.Nil(t, gotErr)

	cbt = base()
	cbt.ports = nil
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "each service should have one associated port")

	cbt = base()
	cbt.svcs = nil
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "each service should have one associated port")

	cbt = base()
	cbt.cluster = "boom"
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "cluster boom not found")

	initialClusterSvcs := testECSMeta.clusterSvcs[cluster1]
	delete(testECSMeta.clusterSvcs, cluster1)
	cbt = base()
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "no services defined for cluster1")
	testECSMeta.clusterSvcs[cluster1] = initialClusterSvcs

	cbt = base()
	cbt.service = "boom"
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "no service boom defined for cluster cluster1")

	cbt = base()
	cbt.ports = []int{apiSvc1Port * 2}
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(
		t,
		gotErr,
		fmt.Sprintf("did not contain a port mapping for %d", apiSvc1Port*2))

	cbt = base()
	cbt.container = "boom"
	gotErr = testECSState.validate(cbt)
	assert.ErrorContains(t, gotErr, "container boom")
}

func TestFindHostPort(t *testing.T) {
	usedPorts := map[arn]map[int]bool{}
	resetPorts := func() {
		for k := range usedPorts {
			delete(usedPorts, k)
		}
	}
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

	carn := arn("carn")
	c := &ecs.Container{
		ContainerArn: ptr.String(string(carn)),
		NetworkBindings: []*ecs.NetworkBinding{
			{ContainerPort: ptr.Int64(80), HostPort: ptr.Int64(8080)},
			{ContainerPort: ptr.Int64(81), HostPort: ptr.Int64(8081)},
			{ContainerPort: ptr.Int64(81), HostPort: ptr.Int64(8082)},
		},
	}

	run := func(c *ecs.Container, d int, want *int, e bool, dontReset ...bool) {
		if len(dontReset) > 1 {
			log.Fatal("called incorrectly, must be length 0 or 1")
		}
		if len(dontReset) == 0 || !dontReset[0] {
			resetPorts()
		}

		got, gotErr := findHostPort(c, d, usePort, isUsed)
		if want != nil {
			assert.Equal(t, got, *want)
		} else {
			assert.NotEqual(t, got, 0)
		}

		if e {
			carn := arn("")
			if c != nil {
				carn = arn(ptr.StringValue(c.ContainerArn))
			}
			assert.DeepEqual(
				t, gotErr, fmt.Errorf("could not bind to port %d in container '%s'", d, carn))
		} else {
			assert.Nil(t, gotErr)
		}
	}

	var (
		pz    = ptr.Int(0)
		p8080 = ptr.Int(8080)
		p8081 = ptr.Int(8081)
		p8082 = ptr.Int(8082)
	)

	// passing nil container
	run(nil, 80, pz, true)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{})

	// successful binding
	run(c, 80, p8080, false)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8080: true}})

	// successful bind multiple times
	run(c, 80, p8080, false)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8080: true}})
	run(c, 80, p8080, false, true)
	run(c, 80, p8080, false, true)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8080: true}})

	// sucessful bind with multiple mappings in the container
	run(c, 81, p8081, false)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8081: true}})
	run(c, 81, p8082, false, true)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8081: true, 8082: true}})
	run(c, 81, nil, false, true)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{carn: {8081: true, 8082: true}})

	// fail to bind a host port (as opposed to a container port)
	run(c, 8080, pz, true)
	assert.DeepEqual(t, usedPorts, map[arn]map[int]bool{})
}

func TestBindClusters(t *testing.T) {
	settings := ecsSettings{clusterTag: tag}
	cbt := testECSMeta.identifyTaggedItems(settings)

	clusters := bindClusters("tbn-cluster", testECSState, cbt)

	md := func(
		cluster string,
		svcarn,
		tdefarn arn,
		taskCont string,
		contInstARN,
		tiarn arn,
		ec2Id string,
		extraLabels map[string]string,
	) api.Metadata {
		mp := map[string]string{
			"ecs-cluster":            cluster,
			"ecs-service":            extractSN(svcarn),
			"ecs-service-arn":        string(svcarn),
			"ecs-task-definition":    string(tdefarn),
			"ecs-task-container":     taskCont,
			"ecs-container-instance": string(contInstARN),
			"ecs-task-instance":      string(tiarn),
			"ec2-instance-id":        ec2Id,
		}

		for k, v := range extraLabels {
			mp[k] = v
		}
		return api.MetadataFromMap(mp)
	}

	wanted := map[string]api.Cluster{
		apiSvc1: {
			Name: apiSvc1,
			Instances: []api.Instance{
				{Host: host1Ip, Port: ti1port1, Metadata: md(
					cluster1, c1s1arn, task1arn, c1c1name, cinst1arn, c1s1_task1arn, host1arn, map[string]string{
						"other": randStr1,
					})},
				{Host: host2Ip, Port: ti2port1, Metadata: md(
					cluster1, c1s1arn, task1arn, c1c1name, cinst2arn, c1s1_task2arn, host2arn, map[string]string{
						"other": randStr1,
					})},
				{Host: host1Ip, Port: ti3port1, Metadata: md(
					cluster1, c1s2arn, task2arn, c1c2name, cinst1arn, c1s2_task1arn, host1arn, map[string]string{
						"other": randStr2,
						"stage": "prod",
					})},
			},
		},
		apiSvc2: {
			Name: apiSvc2,
			Instances: []api.Instance{
				{Host: host1Ip, Port: ti1port2, Metadata: md(
					cluster1, c1s1arn, task1arn, c1c1name, cinst1arn, c1s1_task1arn, host1arn, map[string]string{
						"other": randStr1,
					})},
				{Host: host2Ip, Port: ti2port2, Metadata: md(
					cluster1, c1s1arn, task1arn, c1c1name, cinst2arn, c1s1_task2arn, host2arn, map[string]string{
						"other": randStr1,
					})},
			},
		},
	}

	seen := map[string]bool{}
	for _, got := range clusters {
		seen[got.Name] = true
		want, ok := wanted[got.Name]
		if !assert.True(t, ok) {
			fmt.Printf("unexpected cluster found '%s'\n", got.Name)
		} else {
			if !assert.True(t, got.Equals(want)) {
				fmt.Printf("mismatched cluster '%s'\n", got.Name)
				gotb, _ := json.MarshalIndent(got, "", "  ")
				wantb, _ := json.MarshalIndent(want, "", "  ")
				fmt.Printf("got:  %s\n", string(gotb))
				fmt.Println()
				fmt.Printf("want: %v\n", string(wantb))
			}
		}
	}

	for name := range wanted {
		if !assert.True(t, seen[name]) {
			fmt.Printf("wanted cluster '%s' but did not find it in results\n", name)
		}
	}
}

func getAwsClientMock(t *testing.T) (*mockAwsClient, func()) {
	ctrl := gomock.NewController(t) // assert.Tracing(t))
	client := newMockAwsClient(ctrl)
	return client, ctrl.Finish
}

func strSliceConv(ss []string) []interface{} {
	is := []interface{}{}
	for _, s := range ss {
		is = append(is, s)
	}
	return is
}

func arnSliceConv(ss []arn) []interface{} {
	is := []interface{}{}
	for _, s := range ss {
		is = append(is, s)
	}
	return is
}

func doTestPopulateMeta(t *testing.T) {
	client, fin := getAwsClientMock(t)
	defer fin()

	var (
		clusterA   = "cluster-a"
		clusterB   = "cluster-b"
		svcA       = arn("svc-a")
		svcA2      = arn("svc-a2")
		svcB       = arn("svc-b")
		svcC       = arn("svc-c")
		taskA1     = arn("task-a:1")
		taskA2     = arn("task-a:2")
		taskB      = arn("task-b:1")
		taskC      = arn("task-c:1")
		clusters   = []string{clusterA, clusterB}
		listSvcErr = map[string]error{
			clusterA: nil,
			clusterA: nil,
		}

		clusterSvcs = map[string][]arn{
			clusterA: {svcA, svcB, svcC},
			clusterB: {svcA2, svcC},
		}
		svcDefs = map[string]map[arn]svcDefn{
			clusterA: {
				svcA: {&ecs.Service{TaskDefinition: ptr.String(string(taskA1))}},
				svcB: {&ecs.Service{TaskDefinition: ptr.String(string(taskB))}},
				svcC: {&ecs.Service{TaskDefinition: ptr.String(string(taskC))}},
			},
			clusterB: {
				svcA2: {&ecs.Service{TaskDefinition: ptr.String(string(taskA2))}},
				svcC:  {&ecs.Service{TaskDefinition: ptr.String(string(taskC))}},
			},
		}
		svcDefErr = map[string]error{}

		taskDefs = map[arn]taskDefn{
			taskA1: {&ecs.TaskDefinition{}},
			taskA2: {&ecs.TaskDefinition{}},
			taskB:  {&ecs.TaskDefinition{}},
			taskC:  {&ecs.TaskDefinition{}},
		}
		taskErrs = map[arn]error{}

		firstErr error
	)

	setFirstErr := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}
	dest := emptyECSState()
	dest.meta.clusters = clusters

	// tracks which task has been processed
	seenTask := map[arn]bool{}
	for _, c := range clusters {
		svcs := clusterSvcs[c]
		listRet := map[string]arn{}
		for _, s := range svcs {
			listRet[string(s)] = s
		}

		e := listSvcErr[c]
		setFirstErr(e)
		client.EXPECT().ListServices(c).Return(listRet, e)

		e = svcDefErr[c]
		setFirstErr(e)

		bound := []arn{}
		for _, s := range svcs {
			bound = append(bound, s)
		}
		unorderedMatch := matcher.PredicateMatcher{
			Name: fmt.Sprintf("unordered slice %v", bound),
			Test: func(in interface{}) bool {
				return assert.HasSameElements(t, in, bound)
			},
		}
		client.EXPECT().ServiceDefinitions(c, unorderedMatch).Return(svcDefs[c], e)

		for _, sd := range svcDefs[c] {
			tdefarn := arn(*sd.TaskDefinition)
			if seenTask[tdefarn] {
				continue
			}
			seenTask[tdefarn] = true
			tdef := taskDefs[tdefarn]
			client.EXPECT().TaskDefinition(tdefarn).Return(tdef, taskErrs[tdefarn])
		}
	}

	err := populateMeta(client, &dest)
	if firstErr != nil {
		assert.Equal(t, err, firstErr)
	} else {
		want := ecsMeta{
			clusters:    clusters,
			clusterSvcs: clusterSvcs,
			services:    svcDefs,
			tasks:       taskDefs,
		}

		assert.DeepEqual(t, dest.meta.clusters, want.clusters)
		for k := range want.clusterSvcs {
			want.clusterSvcs[k] = sortDedupeARNs(want.clusterSvcs[k])
			if _, ok := dest.meta.clusterSvcs[k]; ok {
				tmp := a2s(dest.meta.clusterSvcs[k])
				sort.Strings(tmp)
				dest.meta.clusterSvcs[k] = s2a(tmp)
			}
		}
		assert.DeepEqual(t, dest.meta.clusterSvcs, want.clusterSvcs)
		assert.DeepEqual(t, dest.meta.services, want.services)
		assert.DeepEqual(t, dest.meta.tasks, want.tasks)
		assert.Nil(t, err)
	}
}

func TestPopulateMeta(t *testing.T) {
	doTestPopulateMeta(t)
}

type matchActor struct {
	name string
	do   func(interface{})
}

func (ma matchActor) Matches(in interface{}) bool {
	ma.do(in)
	return true
}

func (ma matchActor) String() string {
	return fmt.Sprintf("matchActor %s", ma.name)
}

func doTestPopulateRunning(t *testing.T) {
	client, fin := getAwsClientMock(t)
	defer fin()

	var (
		clusterA = "cluster-a"
		clusterB = "cluster-b"
		svcA     = arn("svc-a")
		svcB     = arn("svc-b")
		svcC     = arn("svc-c")
		svcD     = arn("svc-d")
		svcE     = arn("svc-e")
		taskA1   = arn("task-a-inst-1")
		taskA2   = arn("task-a-inst-2")
		taskC1   = arn("task-c-inst-1")
		taskC2   = arn("task-c-inst-2")
		taskD1   = arn("task-d-inst-1")
		taskD2   = arn("task-d-inst-2")
		taskE1   = arn("task-e-inst-1")
		cinst1   = arn("container-instance-1")
		cinst2   = arn("container-instance-2")
		cinst3   = arn("container-instance-3")
		cinst4   = arn("container-instance-4")
		host1    = "host-arn-1"
		host2    = "host-arn-2"
		host3    = "host-arn-3"
		host1IP  = "10.0.1.15"
		host2IP  = "10.0.1.16"
		host3IP  = "10.0.1.17"

		clusters = []string{clusterA, clusterB}

		clusterSvcs = map[string][]arn{
			clusterA: {svcA, svcB, svcC},
			clusterB: {svcD, svcE},
		}

		clusterSvcTasks = map[string]map[arn][]arn{
			clusterA: {
				svcA: {taskA1, taskA2},
				svcB: {},
				svcC: {taskC1, taskC2},
			},
			clusterB: {
				svcD: {taskD1, taskD2},
				svcE: {taskE1},
			},
		}

		clusterSvcTasksErr    = map[string]map[arn]error{}
		getClusterSvcTasksErr = func(c string, s arn) error {
			se, ok := clusterSvcTasksErr[c]
			if !ok {
				return nil
			}
			return se[s]
		}

		clusterGetTasksErr = map[string]error{}

		taskInstances = map[arn]taskInst{
			taskA1: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst1))}},
			taskA2: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst3))}},

			taskC1: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst1))}},
			taskC2: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst3))}},

			taskD1: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst2))}},
			taskD2: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst4))}},

			taskE1: {&ecs.Task{ContainerInstanceArn: ptr.String(string(cinst4))}},
		}

		clusterContainerInst = map[string][]arn{
			clusterA: {cinst1, cinst3},
			clusterB: {cinst2, cinst4},
		}
		clusterContainerInstErr = map[string]error{}

		containerInstances = map[arn]containerInst{
			cinst1: {&ecs.ContainerInstance{Ec2InstanceId: &host1}},
			cinst2: {&ecs.ContainerInstance{Ec2InstanceId: &host2}},
			cinst3: {&ecs.ContainerInstance{Ec2InstanceId: &host2}},
			cinst4: {&ecs.ContainerInstance{Ec2InstanceId: &host3}},
		}

		hosts = map[string]ec2Instance{
			host1: {&ec2.Instance{PrivateIpAddress: &host1IP}},
			host2: {&ec2.Instance{PrivateIpAddress: &host2IP}},
			host3: {&ec2.Instance{PrivateIpAddress: &host3IP}},
		}

		hostIDs    = []string{host1, host2, host3}
		hostGetErr error

		firstErr error
	)

	setFirstErr := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}

	state := emptyECSState()
	state.meta.clusters = clusters
	state.meta.clusterSvcs = clusterSvcs

	for _, c := range clusters {
		for _, svc := range clusterSvcs[c] {
			tasks := []arn{}
			for _, v := range clusterSvcTasks[c][svc] {
				tasks = append(tasks, v)
			}
			taskErr := getClusterSvcTasksErr(c, svc)
			client.EXPECT().ListTasks(c, svc).Return(tasks, taskErr)
			setFirstErr(taskErr)

			getTaskResult := map[arn]taskInst{}
			for _, tid := range tasks {
				getTaskResult[tid] = taskInstances[tid]
			}
			getTasksErr := clusterGetTasksErr[c]

			client.EXPECT().GetTasks(
				gomock.Any(), c, arnSliceConv(tasks)...,
			).Return(getTaskResult, getTasksErr)
			setFirstErr(getTasksErr)
		}

		getCIResult := map[arn]containerInst{}
		for _, ciid := range clusterContainerInst[c] {
			getCIResult[ciid] = containerInstances[ciid]
		}

		instsErr := clusterContainerInstErr[c]
		client.EXPECT().GetContainerInstances(
			gomock.Any(),
			c,
			arnSliceConv(clusterContainerInst[c])...,
		).Return(getCIResult, instsErr)
		setFirstErr(instsErr)
	}

	client.EXPECT().GetEC2Instances(
		strSliceConv(hostIDs)...,
	).Return(hosts, hostGetErr)

	setFirstErr(hostGetErr)

	gotErr := populateRunning(client, &state)
	if firstErr != nil {
		assert.Equal(t, gotErr, firstErr)
	} else {
		want := ecsRunning{
			svcTasks:           clusterSvcTasks,
			taskInstances:      taskInstances,
			containerInstances: containerInstances,
			ec2Hosts:           hosts,
		}

		assert.DeepEqual(t, state.live, want)
		assert.Nil(t, gotErr)
	}
}

func TestPopulateRunning(t *testing.T) {
	doTestPopulateRunning(t)
}
