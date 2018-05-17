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

// Package consul provides an integration with Consul. See
// "rotor help consul" for usage.
package consul

import (
	"fmt"

	consulapi "github.com/hashicorp/consul/api"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/nonstdlib/arrays/indexof"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/flag/usage"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type consulClient,catalogInterface,healthInterface,getClientInterface -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

// consulClient enables us to provide a mockable interface to the Consul API.
// For documentation of these methods see github.com/hashicorp/consul/tree/master/api docs.
type consulClient interface {
	Catalog() catalogInterface
	Health() healthInterface
}

type ccAdapter struct {
	underlying *consulapi.Client
}

func (a ccAdapter) Catalog() catalogInterface {
	return a.underlying.Catalog()
}

func (a ccAdapter) Health() healthInterface {
	return a.underlying.Health()
}

// catalogInterface enables us to provide a mockable interface to the Consul
// Catalog API. For documentation of these methods see github.com/hashicorp/consul/tree/master/api
// docs.
type catalogInterface interface {
	Datacenters() ([]string, error)
	Services(queryOpts *consulapi.QueryOptions) (map[string][]string, *consulapi.QueryMeta, error)
	Service(
		service, tag string,
		queryOpts *consulapi.QueryOptions,
	) ([]*consulapi.CatalogService, *consulapi.QueryMeta, error)
}

// healthInterface enables us to provide a mockable interface to the Consul
// Health API. For documentation of these methods see github.com/hashicorp/consul/tree/master/api
// docs.
type healthInterface interface {
	Node(
		node string,
		queryOpts *consulapi.QueryOptions,
	) (consulapi.HealthChecks, *consulapi.QueryMeta, error)
}

const (
	defaultClusterTag = "tbn-cluster"
	defaultConsulHost = "localhost:8500"
	consulDescription = `Connects to a Consul agent via HTTP API and updates
Clusters stored in the Turbine Labs API at startup and periodically thereafter.

A service is marked for import using tags, by default "` + defaultClusterTag + `"
is used but it may be customized through the command line (see -cluster-tag).
Each identified service will be imported as a Turbine Cluster and the nodes
that are marked with the configured tag are added as instances for that
Cluster.  Additional tags on a node are added as instance metadata, the service
tag itself is not included.

Node health checks will be added as instance metadata named following the pattern
"check:<check-id>" with the check status as value. Additionally "node-health" is
added for an instance within each cluster to aggregate all the other health
checks on that node that either are 1) not bound to a service or 2) bound to
the service this cluster represents. The value for this aggregate metadata will be:

    passing   if all Consul health checks have a "passing" value
    mixed     if any Consul health check has a "passing" value
    failed    if no Consul health check has the value of "passing"
`
)

type consulEndpointConfig struct {
	useSSL bool
	host   string
}

func (c consulEndpointConfig) getClient() (consulClient, error) {
	scheme := "http"
	if c.useSSL {
		scheme = "https"
	}

	cfg := &consulapi.Config{
		Address: c.host,
		Scheme:  scheme,
	}

	client, e := consulapi.NewClient(cfg)
	return ccAdapter{client}, e
}

type consulSettings struct {
	tbnServiceTag string
	consulDC      string
	endpoint      getClientInterface
}

type consulUpdateFn func(updater.Updater, consulClient, string, string)

type consulRunner struct {
	consulSettings

	updaterFlags rotor.UpdaterFromFlags
}

func Cmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	runner := &consulRunner{}
	runner.updaterFlags = updaterFlags

	cmd := &command.Cmd{
		Name:        "consul",
		Summary:     "Consul collector",
		Usage:       "[OPTIONS]",
		Description: consulDescription,
		Runner:      runner,
	}

	flags := tbnflag.Wrap(&cmd.Flags)

	console.Init(flags)

	flags.StringVar(
		&runner.consulDC,
		"dc",
		"",
		usage.Required("Collect Consul services only from this DC."))

	flags.StringVar(
		&runner.tbnServiceTag,
		"cluster-tag",
		defaultClusterTag,
		"The tag used to indicate that a service should be imported as a Cluster.")

	endpoint := consulEndpointConfig{}

	flags.BoolVar(
		&endpoint.useSSL,
		"use-ssl",
		false,
		"If set will instruct communications to the Consul API to be done via SSL.")

	flags.StringVar(
		&endpoint.host,
		"hostport",
		defaultConsulHost,
		"The `[host]:port` for the Consul API.")

	runner.endpoint = &endpoint

	return cmd
}

type getClientInterface interface {
	getClient() (consulClient, error)
}

func (cr *consulRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if cr.consulDC == "" {
		return cmd.BadInput("Target datacenter must be specified.")
	}

	if err := cr.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	u, err := cr.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	client, err := cr.endpoint.getClient()
	if err != nil {
		return cmd.Error(err)
	}

	dcs, err := getConsulDatacenters(client.Catalog())
	if err != nil {
		return cmd.Error(err)
	}

	if indexof.String(dcs, cr.consulDC) == -1 {
		return cmd.Errorf("Datacenter %s was not found", cr.consulDC)
	}

	updater.Loop(
		u,
		func() ([]api.Cluster, error) {
			return consulGetClusters(
				client,
				cr.tbnServiceTag,
				cr.consulDC,
				getConsulServices,
				getConsulServiceDetail,
				getConsulNodeHealth,
				mkClusters,
			)
		},
	)

	return command.NoError()
}

func consulGetClusters(
	client consulClient,
	svcTag string,
	dc string,
	getServices getConsulServicesFn,
	getServiceDetail getConsulServiceDetailFn,
	getNodeHealth getConsulNodeHealthFn,
	getClusters mkClusterFn,
) ([]api.Cluster, error) {
	healthAPI := client.Health()
	catalogAPI := client.Catalog()

	// get services within a datacenter
	svcs, err := getServices(catalogAPI, dc)
	if err != nil {
		return nil, fmt.Errorf(
			"Skipping collection. Error retrieving services in data center %s: %s",
			dc,
			err.Error(),
		)
	}

	// cull to only services marked for turbine
	svcs = svcs.FindWithTag(svcTag)

	// get nodes for a service
	svcDetails := map[string]consulServiceDetail{}
	for svcID := range svcs {
		detail, err := getServiceDetail(catalogAPI, dc, svcID, svcTag)
		if err != nil {
			return nil, fmt.Errorf(
				"Skipping collection. Error retrieving nodes for service %s: %s",
				svcID,
				err.Error(),
			)
		}
		svcDetails[svcID] = detail
	}

	// get health checks for a node
	nodeHealth := map[string]nodeHealth{}
	for _, details := range svcDetails {
		for _, node := range details.nodes {
			// skip this node if it isn't tagged for inclusion
			if indexof.String(node.tags, svcTag) == -1 {
				continue
			}

			if _, haveNode := nodeHealth[node.id]; !haveNode {
				health, err := getNodeHealth(healthAPI, dc, node.id)
				if err != nil {
					return nil, fmt.Errorf(
						"Skipping collection. Error getting node health %s: %s",
						node.id,
						err.Error(),
					)
				}
				nodeHealth[node.id] = health
			}
		}
	}

	return getClusters(svcTag, svcDetails, nodeHealth), nil
}

// mkClusterFn represents a function that combines service details and node
// health information from Consul into a collection of Turbine Clusters.
type mkClusterFn func(string, map[string]consulServiceDetail, map[string]nodeHealth) api.Clusters

func mkClusters(
	svcTag string,
	svcDetails map[string]consulServiceDetail,
	nodeHealth map[string]nodeHealth,
) api.Clusters {
	result := []api.Cluster{}

	for svc, detail := range svcDetails {
		c := api.Cluster{Name: svc}

		for _, node := range detail.nodes {
			// core instance data
			inst := api.Instance{
				Host: node.address,
				Port: node.port,
			}

			// add metadata
			metadata := map[string]string{
				"node-id": node.id,
			}

			for _, t := range node.tags {
				if t == svcTag {
					continue
				}
				metadata["tag:"+t] = ""
			}

			for k, v := range node.meta {
				metadata[k] = v
			}

			health := nodeHealth[node.id]
			allChecks := 0
			healthyChecks := 0
			for _, chk := range health {
				if chk.ServiceID != "" && chk.ServiceID != svc {
					continue
				}
				checkName := "check:" + chk.CheckID
				metadata[checkName] = chk.Status
				allChecks++
				if chk.Status == consulapi.HealthPassing {
					healthyChecks++
				}
			}

			if allChecks == healthyChecks {
				metadata["node-health"] = consulapi.HealthPassing
			}
			if healthyChecks > 0 && healthyChecks < allChecks {
				metadata["node-health"] = "mixed"
			}
			if healthyChecks == 0 && allChecks > 0 {
				metadata["node-health"] = "failed"
			}

			inst.Metadata = api.MetadataFromMap(metadata)

			// append new instance
			c.Instances = append(c.Instances, inst)
		}

		result = append(result, c)
	}

	return result
}

func getConsulDatacenters(client catalogInterface) ([]string, error) {
	dcs, err := client.Datacenters()
	if err != nil {
		return nil, err
	}

	return dcs, nil
}

// serviceListing maps service id -> aggregate service tag set
type serviceListing map[string][]string

func (sl serviceListing) FindWithTag(t string) serviceListing {
	results := serviceListing{}
	for k, v := range sl {
		if indexof.String(v, t) != -1 {
			results[k] = v
		}
	}

	return results
}

// getConsulServicesFn takes a Consul catalog interface and a datacenter
// returning a map from service name to tags that are applied to that
// service.
type getConsulServicesFn func(catalogInterface, string) (serviceListing, error)

func getConsulServices(client catalogInterface, dc string) (serviceListing, error) {
	opts := &consulapi.QueryOptions{Datacenter: dc, RequireConsistent: true}
	svcs, _, err := client.Services(opts)
	if err != nil {
		return nil, err
	}

	return serviceListing(svcs), nil
}

type consulServiceNode struct {
	id      string
	address string
	port    int
	tags    []string
	meta    map[string]string
}

func (n consulServiceNode) HasTag(t string) bool {
	return indexof.String(n.tags, t) != -1
}

type consulServiceDetail struct {
	id    string
	nodes []consulServiceNode
}

func serviceDetailFromSvcs(name string, svcs []*consulapi.CatalogService) consulServiceDetail {
	result := consulServiceDetail{name, nil}

	for _, s := range svcs {
		n := consulServiceNode{}
		n.id = s.Node
		n.tags = make([]string, len(s.ServiceTags))
		copy(n.tags, s.ServiceTags)
		if s.ServiceAddress != "" {
			n.address = s.ServiceAddress
		} else {
			n.address = s.Address
		}
		n.meta = s.NodeMeta
		n.port = s.ServicePort

		result.nodes = append(result.nodes, n)
	}

	return result
}

// getConsulServiceDetailFn takes a catalog interface, datacenter, service id,
// and the service tag we use to identify Turbine services. From this it returns
// a condensed version of the Consul view of the service including a list of
// nodes that are part of it.
type getConsulServiceDetailFn func(
	client catalogInterface, dc, svcName, svcTag string) (consulServiceDetail, error)

func getConsulServiceDetail(
	client catalogInterface,
	dc,
	svcName,
	svcTag string,
) (consulServiceDetail, error) {
	opts := &consulapi.QueryOptions{Datacenter: dc, RequireConsistent: true}
	svcs, _, err := client.Service(svcName, svcTag, opts)
	if err != nil {
		return consulServiceDetail{}, err
	}

	result := serviceDetailFromSvcs(svcName, svcs)
	return result, nil
}

type nodeHealth []*consulapi.HealthCheck

// getConsulNodeHealthFn takes a health interface, a datacenter, and a node id.
// From these it produces a list of health checks associted with that node.
type getConsulNodeHealthFn func(client healthInterface, dc, node string) (nodeHealth, error)

func getConsulNodeHealth(client healthInterface, dc, node string) (nodeHealth, error) {
	opts := &consulapi.QueryOptions{Datacenter: dc, RequireConsistent: true}
	health, _, err := client.Node(node, opts)
	if err != nil {
		return nil, err
	}

	return nodeHealth(health), nil
}
