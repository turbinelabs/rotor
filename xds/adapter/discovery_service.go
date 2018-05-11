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
	"fmt"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/collector"
)

type discoveryService interface {
	Fetch(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error)
	Close() error
}

type fnDiscoveryService struct {
	fetchFn func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error)
	closeFn func() error
}

func (ds fnDiscoveryService) Fetch(
	req *envoyapi.DiscoveryRequest,
) (*envoyapi.DiscoveryResponse, error) {
	return ds.fetchFn(req)
}

func (ds fnDiscoveryService) Close() error { return ds.closeFn() }

type clusterService discoveryService
type endpointService discoveryService

type requestFactory func() *envoyapi.DiscoveryRequest

var mkEmptyRequest = func() *envoyapi.DiscoveryRequest {
	return &envoyapi.DiscoveryRequest{}
}

func fetchClusters(
	cs clusterService,
	req *envoyapi.DiscoveryRequest,
) ([]*envoyapi.Cluster, error) {
	res, err := cs.Fetch(req)
	if err != nil {
		return nil, err
	}

	clusters := make([]*envoyapi.Cluster, len(res.GetResources()))
	for idx, any := range res.GetResources() {
		c := &envoyapi.Cluster{}
		if err := types.UnmarshalAny(&any, c); err != nil {
			return nil, err
		}

		clusters[idx] = c
	}

	return clusters, nil
}

func asClusterResolver(es endpointService, rf requestFactory) collector.ClusterResolver {
	return func(n string) (api.Instances, []error) {
		var req *envoyapi.DiscoveryRequest
		if rf == nil {
			req = &envoyapi.DiscoveryRequest{}
		} else if req = rf(); req == nil {
			req = &envoyapi.DiscoveryRequest{}
		}

		req.ResourceNames = []string{n}
		res, err := es.Fetch(req)
		if err != nil {
			return nil, []error{err}
		}

		var cla *envoyapi.ClusterLoadAssignment

		switch {
		case len(res.GetResources()) == 0:
			return nil, nil

		case len(res.GetResources()) > 1:
			return nil, []error{fmt.Errorf("Multiple ClusterLoadAssigments found for service %s", n)}

		default:
			any := res.GetResources()[0]
			cla = &envoyapi.ClusterLoadAssignment{}
			if err := types.UnmarshalAny(&any, cla); err != nil {
				return nil, []error{err}
			}
		}

		return envoyEndpointsToTbnInstances(cla.GetEndpoints())
	}
}
