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

// Package kubernetes provides an integration with Kubernetes. See
// "rotor help kubernetes" for usage.
package kubernetes

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	k8sapiv1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8s "k8s.io/client-go/kubernetes"
	k8stypedv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/updater"
)

const (
	// NodeNameLabel is the label used to store the kubernetes node name
	NodeNameLabel = "tbn_k8s_node_name"

	// HostIPLabel is the label used to store the kubernetes host ip
	HostIPLabel = "tbn_k8s_host_ip"

	k8sMetadataPrefix = "kubernetes.io/"

	k8sDescription = `Connects to a Kubernetes cluster API server and
updates Clusters stored in the Turbine Labs API at startup and periodically
thereafter.  By default, the tool assumes that it is being run within the
Kubernetes cluster and will automatically find the API server.

Pod labels are used to determine to which API cluster a particular pod belongs.
The default label name is "` + constants.DefaultClusterLabelName + `", but it may be
overridden by command line flags (see -cluster-label). By default all pods in
the configured namespace are watched, but you may also provide a label selector
(using the same format at the kubectl command) to specify a subset of pods to
watch.

In each pod, all containers must be running before the pod is considered live
and ready for inclusion in the API cluster's instance list. Each container is
examined for ports. The first TCP port found is used as the API instance's port
unless a port name is specified (see -port-name), in which case the first port
with that name becomes the API instance's port. Pods with no container port are
ignored. All pod labels and annotations (except for the cluster label) are
attached as instance metadata.`
)

func Cmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	runner := &kubernetesRunner{}

	cmd := &command.Cmd{
		Name:        "kubernetes",
		Summary:     "kubernetes collector",
		Usage:       "[OPTIONS]",
		Description: k8sDescription,
		Runner:      runner,
	}

	cmd.Flags.StringVar(
		&runner.namespace,
		"namespace",
		"default",
		"The Kubernetes cluster `namespace` to watch for pods.")

	cmd.Flags.StringVar(
		&runner.selector,
		"selector",
		"",
		"A Kubernetes label selector that selects which pods are polled.")

	cmd.Flags.StringVar(
		&runner.clusterNameLabel,
		"cluster-label",
		constants.DefaultClusterLabelName,
		"The `name` of Kubernetes label that specifies to which cluster a pod belongs.")

	cmd.Flags.StringVar(
		&runner.portName,
		"port-name",
		"http",
		"The named container port assigned to cluster instances.")

	cmd.Flags.DurationVar(
		&runner.timeout,
		"timeout",
		120*time.Second,
		"The timeout used for Kubernetes API requests (converted to seconds).")

	runner.k8sClientFlags = newClientFromFlags(tbnflag.Wrap(&cmd.Flags))
	runner.updaterFlags = updaterFlags

	return cmd
}

type k8sCollectorSettings struct {
	namespace        string
	selector         string
	clusterNameLabel string
	portName         string
	timeout          time.Duration
}

type kubernetesRunner struct {
	k8sCollectorSettings

	k8sClientFlags clientFromFlags
	updaterFlags   rotor.UpdaterFromFlags
}

func (r *kubernetesRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := r.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err.Error())
	}

	var labelSelector labels.Selector
	if r.selector == "" {
		labelSelector = labels.NewSelector()
	} else {
		var err error
		labelSelector, err = labels.Parse(r.selector)
		if err != nil {
			return cmd.BadInputf("Error parsing selector: %s", err.Error())
		}
	}

	u, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Errorf(err.Error())
	}

	k8sClient, err := r.k8sClientFlags.Make()
	if err != nil {
		return cmd.Errorf("Unable to instantiate kubernetes client: %s", err.Error())
	}

	c := kubernetesCollector{
		k8sCollectorSettings: r.k8sCollectorSettings,
		k8sClient:            k8sClient,
		labelSelector:        labelSelector,
	}

	clientPods := c.k8sClient.Core().Pods(c.namespace)
	updater.Loop(
		u,
		func() ([]api.Cluster, error) {
			return c.getClusters(clientPods)
		},
	)

	return command.NoError()
}

type kubernetesCollector struct {
	k8sCollectorSettings

	k8sClient     *k8s.Clientset
	labelSelector labels.Selector
}

func (c *kubernetesCollector) getClusters(client k8stypedv1.PodInterface) (api.Clusters, error) {
	timeout := int64(math.Max(c.timeout.Seconds(), 1.0))
	listOptions := k8smetav1.ListOptions{
		LabelSelector:  c.labelSelector.String(),
		TimeoutSeconds: &timeout,
	}

	list, err := client.List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("error executing kubernetes api list: %s", err.Error())
	}

	clustersMap := map[string]*api.Cluster{}
	for _, pod := range list.Items {
		c.handlePod(clustersMap, pod)
	}

	clusters := make(api.Clusters, 0, len(clustersMap))
	for _, cluster := range clustersMap {
		clusters = append(clusters, *cluster)
	}

	return clusters, nil
}

func (c *kubernetesCollector) makeInstance(pod k8sapiv1.Pod, port int) (string, api.Instance) {
	debug := console.Debug()

	host := pod.Status.PodIP
	lbls := pod.GetLabels()
	ans := pod.GetAnnotations()

	clusterName := lbls[c.clusterNameLabel]
	if clusterName == "" {
		debug.Printf("Skipped Instance %s:%d: missing/empty cluster label", host, port)
		return "", api.Instance{}
	}

	debug.Printf("Saw Instance %s:%d for Cluster %s", host, port, clusterName)

	metadata := api.Metadata{}

	for key, value := range lbls {
		if key != c.clusterNameLabel {
			metadata = append(metadata, api.Metadatum{Key: key, Value: value})
		}
	}

	for key, value := range ans {
		if !strings.HasPrefix(key, k8sMetadataPrefix) {
			metadata = append(metadata, api.Metadatum{Key: key, Value: value})
		}
	}

	if pod.Status.HostIP != "" {
		metadata = append(metadata, api.Metadatum{Key: HostIPLabel, Value: pod.Status.HostIP})
	}

	if pod.Spec.NodeName != "" {
		metadata = append(metadata, api.Metadatum{Key: NodeNameLabel, Value: pod.Spec.NodeName})
	}

	return clusterName, api.Instance{
		Host:     host,
		Port:     port,
		Metadata: metadata,
	}
}

func (c *kubernetesCollector) isContainerRunning(pod k8sapiv1.Pod) bool {
	if len(pod.Status.ContainerStatuses) == 0 {
		return false
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Running == nil || !cs.Ready {
			return false
		}
	}

	return true
}

func (c *kubernetesCollector) findContainerPort(pod k8sapiv1.Pod) (int, error) {
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Protocol == k8sapiv1.ProtocolTCP {
				if c.portName == "" || port.Name == c.portName {
					return int(port.ContainerPort), nil
				}
			}
		}
	}

	return -1, errors.New("unable to find container port")
}

func (c *kubernetesCollector) handlePod(clusters map[string]*api.Cluster, pod k8sapiv1.Pod) {
	port, err := c.findContainerPort(pod)
	if err != nil {
		// port may be unassigned at startup or shutdown, ignore its absence
		return
	}

	if !c.isContainerRunning(pod) {
		return
	}

	clusterName, instance := c.makeInstance(pod, port)

	if clusterName == "" {
		return
	}

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
