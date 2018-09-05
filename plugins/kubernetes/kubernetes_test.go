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

package kubernetes

//go:generate mockgen -destination mock_pod_iface_test.go --write_package_comment=false -package $GOPACKAGE k8s.io/client-go/kubernetes/typed/core/v1 PodInterface

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	k8sapiv1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"
	testlog "github.com/turbinelabs/test/log"
)

const (
	podIP   = "10.0.0.1"
	podPort = 80
)

var (
	podMetadata = api.Metadata{
		{Key: "app", Value: "www"},
	}
)

func makePod(
	clusterName string,
	hasPort bool,
	portName string,
	ready bool,
) k8sapiv1.Pod {
	var ports []k8sapiv1.ContainerPort
	if hasPort {
		ports = []k8sapiv1.ContainerPort{
			{Protocol: k8sapiv1.ProtocolTCP, ContainerPort: podPort, Name: portName},
		}
	}

	pod := k8sapiv1.Pod{
		ObjectMeta: k8smetav1.ObjectMeta{
			Labels: map[string]string{
				"clusterName": clusterName,
				"app":         "www",
			},
		},
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{Ports: ports},
			},
		},
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: ready,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
			PodIP: "10.0.0.1",
		},
	}
	pod.SetName(fmt.Sprintf("%s-%t-%t", clusterName, hasPort, ready))
	pod.SetNamespace("namespace")
	return pod
}

func TestKubernetesHandlePod(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "clusterName"},
		debugLog:             debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := api.Cluster{
		Name: "cluster1",
		Instances: []api.Instance{
			{Host: podIP, Port: podPort, Metadata: podMetadata},
		},
	}

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", true, "", true)

	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Adding port 80 on pod \"namespace.cluster1-true-true\", because it was the first port encountered and --port-name is empty. To use a named port, set --port-name\nAdding pod \"namespace.cluster1-true-true\" (10.0.0.1:80) in Cluster cluster1\n")
}

func TestKubernetesHandlePodSkipOnNoPorts(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{
			clusterNameLabel: "clusterName",
			portName:         "",
		},
		debugLog: debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := cluster

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", false, "", true)

	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Ignoring pod \"namespace.cluster1-false-true\", because it exposes no ports\n")
}

func TestKubernetesHandlePodSkipOnMissingPort(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{
			clusterNameLabel: "clusterName",
			portName:         "portName",
		},
		debugLog: debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := cluster

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", false, "", true)

	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Ignoring pod \"namespace.cluster1-false-true\", because it has no port named \"portName\" (can be configured with --port-name).\n")
}

func TestKubernetesHandlePodSkipOnDifferentlyNamedPort(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{
			clusterNameLabel: "clusterName",
			portName:         "portName",
		},
		debugLog: debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := cluster

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", false, "otherName", true)
	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Ignoring pod \"namespace.cluster1-false-true\", because it has no port named \"portName\" (can be configured with --port-name).\n")
}

func TestKubernetesHandlePodSkipOnNotReady(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "clusterName"},
		debugLog:             debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := cluster

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", true, "", false)

	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Adding port 80 on pod \"namespace.cluster1-true-false\", because it was the first port encountered and --port-name is empty. To use a named port, set --port-name\nIgnoring pod \"namespace.cluster1-true-false\", because it has at least one non-running container.\n")
}

func TestKubernetesHandlePodSkipOnMissingCluster(t *testing.T) {
	debug, logBuffer := testlog.NewBufferLogger()
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "missingLabel"},
		debugLog:             debug,
	}

	cluster := api.Cluster{
		Name:      "cluster1",
		Instances: []api.Instance{},
	}

	expectedCluster := cluster

	clusters := map[string]*api.Cluster{
		"cluster1": &cluster,
	}

	pod := makePod("cluster1", true, "", true)

	collector.handlePod(clusters, pod)

	assert.DeepEqual(t, clusters, map[string]*api.Cluster{"cluster1": &expectedCluster})
	assert.Equal(t, logBuffer.String(), "Adding port 80 on pod \"namespace.cluster1-true-true\", because it was the first port encountered and --port-name is empty. To use a named port, set --port-name\nSkipped pod \"namespace.cluster1-true-true\": missing/empty cluster label\n")
}

func TestKubernetesIsContainerRunning(t *testing.T) {
	collector := kubernetesCollector{}

	pod := k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{},
		},
	}

	assert.False(t, collector.isContainerRunning(pod))

	pod = k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: false,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	assert.False(t, collector.isContainerRunning(pod))

	pod = k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Waiting: &k8sapiv1.ContainerStateWaiting{},
					},
				},
			},
		},
	}

	assert.False(t, collector.isContainerRunning(pod))

	pod = k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	assert.True(t, collector.isContainerRunning(pod))

	pod = k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
				{
					Ready: false,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	assert.False(t, collector.isContainerRunning(pod))

	pod = k8sapiv1.Pod{
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	assert.True(t, collector.isContainerRunning(pod))
}

func TestKubernetesFindContainerPort(t *testing.T) {
	debug, _ := testlog.NewBufferLogger()
	collector := kubernetesCollector{debugLog: debug}

	pod := k8sapiv1.Pod{
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{},
		},
	}

	port := collector.findContainerPort(pod)
	assert.Nil(t, port)

	pod = k8sapiv1.Pod{
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{
					Ports: []k8sapiv1.ContainerPort{
						{
							Protocol:      k8sapiv1.ProtocolUDP,
							ContainerPort: 100,
						},
					},
				},
			},
		},
	}

	port = collector.findContainerPort(pod)
	assert.Nil(t, port)

	pod = k8sapiv1.Pod{
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{
					Ports: []k8sapiv1.ContainerPort{
						{
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
	}

	port = collector.findContainerPort(pod)
	if assert.NonNil(t, port) {
		assert.Equal(t, *port, 80)
	}

	pod = k8sapiv1.Pod{
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{
					Ports: []k8sapiv1.ContainerPort{
						{
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 80,
						},
						{
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 100,
						},
					},
				},
			},
		},
	}

	port = collector.findContainerPort(pod)
	if assert.NonNil(t, port) {
		assert.Equal(t, *port, 80)
	}

	pod = k8sapiv1.Pod{
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{
					Ports: []k8sapiv1.ContainerPort{
						{
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 80,
						},
						{
							Name:          "pickme",
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 100,
						},
					},
				},
			},
		},
	}

	collector.portName = "pickme"
	port = collector.findContainerPort(pod)
	if assert.NonNil(t, port) {
		assert.Equal(t, *port, 100)
	}
}

func TestKubernetesMakeInstance(t *testing.T) {
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "cname"},
	}

	clusterName, instance := collector.makeInstance(
		k8sapiv1.Pod{
			Status: k8sapiv1.PodStatus{PodIP: "pod ip"},
			ObjectMeta: k8smetav1.ObjectMeta{
				Labels: map[string]string{"cname": "kluster"},
			},
		},
		8000,
	)

	assert.Equal(t, clusterName, "kluster")

	expectedInstance := api.Instance{
		Host:     "pod ip",
		Port:     8000,
		Metadata: api.Metadata{},
	}
	assert.DeepEqual(t, instance, expectedInstance)
}

func TestKubernetesMakeInstanceNoCluster(t *testing.T) {
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "cname"},
	}
	clusterName, instance := collector.makeInstance(
		k8sapiv1.Pod{
			Status: k8sapiv1.PodStatus{PodIP: "pod ip"},
			ObjectMeta: k8smetav1.ObjectMeta{
				Labels: map[string]string{"cname": ""},
			},
		},
		8000,
	)
	assert.Equal(t, clusterName, "")
	assert.DeepEqual(t, instance, api.Instance{})

	clusterName, instance = collector.makeInstance(k8sapiv1.Pod{}, 8000)
	assert.Equal(t, clusterName, "")
	assert.DeepEqual(t, instance, api.Instance{})
}

func TestKubernetesMakeInstanceWithLabels(t *testing.T) {
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "cname"},
	}
	clusterName, instance := collector.makeInstance(
		k8sapiv1.Pod{
			Status: k8sapiv1.PodStatus{PodIP: "pod ip"},
			ObjectMeta: k8smetav1.ObjectMeta{
				Labels: map[string]string{
					"cname": "kluster",
					"app":   "www",
					"stage": "production",
				},
			},
		},
		8000,
	)
	assert.Equal(t, clusterName, "kluster")

	expectedInstance := api.Instance{
		Host: "pod ip",
		Port: 8000,
		Metadata: api.Metadata{
			{Key: "app", Value: "www"},
			{Key: "stage", Value: "production"},
		},
	}
	assert.Equal(t, instance.Host, expectedInstance.Host)
	assert.Equal(t, instance.Port, expectedInstance.Port)
	assert.HasSameElements(t, instance.Metadata, expectedInstance.Metadata)
}

func TestKubernetesMakeInstanceWithExtras(t *testing.T) {
	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "cname"},
	}
	clusterName, instance := collector.makeInstance(
		k8sapiv1.Pod{
			Spec:   k8sapiv1.PodSpec{NodeName: "node name"},
			Status: k8sapiv1.PodStatus{PodIP: "pod ip", HostIP: "host ip"},
			ObjectMeta: k8smetav1.ObjectMeta{
				Labels: map[string]string{
					"cname":     "kluster",
					"label key": "label value",
				},
				Annotations: map[string]string{
					"anno key": "anno value",
				},
			},
		},
		8000,
	)
	assert.Equal(t, clusterName, "kluster")

	expectedInstance := api.Instance{
		Host: "pod ip",
		Port: 8000,
		Metadata: api.Metadata{
			{Key: "label key", Value: "label value"},
			{Key: NodeNameLabel, Value: "node name"},
			{Key: HostIPLabel, Value: "host ip"},
		},
	}
	assert.Equal(t, instance.Host, expectedInstance.Host)
	assert.Equal(t, instance.Port, expectedInstance.Port)
	assert.HasSameElements(t, instance.Metadata, expectedInstance.Metadata)
}

func TestKubernetesGetClusters(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	labelSelector, err := labels.Parse("app=foo")
	assert.Nil(t, err)

	collector := kubernetesCollector{
		k8sCollectorSettings: k8sCollectorSettings{clusterNameLabel: "clusterName"},
		labelSelector:        labelSelector,
	}

	pod := k8sapiv1.Pod{
		ObjectMeta: k8smetav1.ObjectMeta{
			Labels: map[string]string{
				"clusterName": "cluster",
				"app":         "www",
			},
		},
		Spec: k8sapiv1.PodSpec{
			Containers: []k8sapiv1.Container{
				{
					Ports: []k8sapiv1.ContainerPort{
						{
							Protocol:      k8sapiv1.ProtocolTCP,
							ContainerPort: 80,
						},
					},
				},
			},
		},
		Status: k8sapiv1.PodStatus{
			ContainerStatuses: []k8sapiv1.ContainerStatus{
				{
					Ready: true,
					State: k8sapiv1.ContainerState{
						Running: &k8sapiv1.ContainerStateRunning{},
					},
				},
			},
			PodIP: "10.0.0.1",
		},
	}

	listOptions := k8smetav1.ListOptions{
		LabelSelector:  labelSelector.String(),
		TimeoutSeconds: ptr.Int64(1),
	}

	podsIface := NewMockPodInterface(ctrl)
	podsIface.EXPECT().List(listOptions).Return(&k8sapiv1.PodList{Items: []k8sapiv1.Pod{pod}}, nil)

	clusters, err := collector.getClusters(podsIface)
	assert.Nil(t, err)
	assert.ArrayEqual(
		t,
		clusters,
		api.Clusters{
			{
				Name: "cluster",
				Instances: []api.Instance{
					{
						Host: "10.0.0.1",
						Port: 80,
						Metadata: api.Metadata{
							api.Metadatum{Key: "app", Value: "www"},
						},
					},
				},
			},
		},
	)
}

func TestCmd(t *testing.T) {
	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(nil)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*kubernetesRunner)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
	assert.NonNil(t, runner.k8sClientFlags)
}

func TestKubernetesRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	kr := kubernetesRunner{updaterFlags: mockUpdaterFromFlags}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(err)

	cmdErr := kr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "kubernetes: "+err.Error())
}

func TestKubernetesRunnerRunMakeUpdaterError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	kr := kubernetesRunner{updaterFlags: mockUpdaterFromFlags}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(nil, err)

	cmdErr := kr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "kubernetes: "+err.Error())
}

func TestKubernetesRunnerRunMakeK8sClientError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockK8sClientFromFlags := newMockClientFromFlags(ctrl)
	mockUpdater := updater.NewMockUpdater(ctrl)

	kr := kubernetesRunner{
		updaterFlags:   mockUpdaterFromFlags,
		k8sClientFlags: mockK8sClientFromFlags,
	}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)
	mockK8sClientFromFlags.EXPECT().Make().Return(nil, err)

	cmdErr := kr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "kubernetes: Unable to instantiate kubernetes client: boom")
}

func TestKubernetesRunnerRunBadLabelSelector(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockK8sClientFromFlags := newMockClientFromFlags(ctrl)

	kr := kubernetesRunner{
		k8sCollectorSettings: k8sCollectorSettings{selector: "=nope"},
		updaterFlags:         mockUpdaterFromFlags,
		k8sClientFlags:       mockK8sClientFromFlags,
	}

	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)

	cmdErr := kr.Run(Cmd(mockUpdaterFromFlags), nil)
	assert.HasPrefix(t, cmdErr.Message, "kubernetes: Error parsing selector: ")
}
