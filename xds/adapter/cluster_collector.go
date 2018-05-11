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

package adapter

import (
	"io"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/collector"
)

type clusterCollector struct {
	io.Closer
	fetchFn     func() ([]*envoyapi.Cluster, error)
	transformFn func([]*envoyapi.Cluster) (api.Clusters, map[string][]error)
}

// NewClusterCollector produces a ClusterCollector which connects to a CDS
// server on the given addr, and collects clusters for the given zone name. If
// isJSON is true, the JSON/REST transport will be used instead of gRPC.
func NewClusterCollector(addr, zoneName string, isJSON bool) (collector.ClusterCollector, error) {
	var (
		cs  clusterService
		err error
	)

	if isJSON {
		cs = newRESTClusterService(addr)
	} else {
		cs, err = newGRPCClusterService(addr)
		if err != nil {
			return nil, err
		}
	}

	requestFactory := func() *envoyapi.DiscoveryRequest {
		return &envoyapi.DiscoveryRequest{
			Node: &envoycore.Node{
				Locality: &envoycore.Locality{Zone: zoneName},
			},
		}
	}

	return clusterCollector{
		Closer: cs,
		fetchFn: func() ([]*envoyapi.Cluster, error) {
			return fetchClusters(cs, requestFactory())
		},
		transformFn: newClusterTransformer(requestFactory),
	}, nil
}

func (c clusterCollector) Collect() (api.Clusters, map[string][]error) {
	cs, err := c.fetchFn()
	if err != nil {
		console.Error().Printf("Error calling CDS: %s", err)
	}

	return c.transformFn(cs)
}
