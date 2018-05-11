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
	"errors"
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

func TestClusterServiceReturnsUnderlyingError(t *testing.T) {
	cs := clusterService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			return nil, errors.New("boom")
		},
	})

	res, err := fetchClusters(cs, mkEmptyRequest())
	assert.Nil(t, res)
	assert.NonNil(t, err)
}

func TestClusterServiceReturnsEmptyResponseFromUnderlying(t *testing.T) {
	cs := clusterService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			return &envoyapi.DiscoveryResponse{}, nil
		},
	})

	res, err := fetchClusters(cs, mkEmptyRequest())
	assert.Nil(t, err)
	assert.NonNil(t, res)
	assert.Equal(t, len(res), 0)
}

func TestClusterServiceUnderlyingResponseParsedAndReturned(t *testing.T) {
	expectedClusters := []*envoyapi.Cluster{
		{
			Name: "cluster1",
			Type: envoyapi.Cluster_STATIC,
			Hosts: []*envoycore.Address{
				{
					Address: &envoycore.Address_SocketAddress{
						SocketAddress: &envoycore.SocketAddress{
							Protocol: envoycore.TCP,
							Address:  "1.2.3.4",
							PortSpecifier: &envoycore.SocketAddress_PortValue{
								PortValue: 1234,
							},
						},
					},
				},
			},
		},
		{
			Name: "cluster2",
			Type: envoyapi.Cluster_STATIC,
			Hosts: []*envoycore.Address{
				{
					Address: &envoycore.Address_SocketAddress{
						SocketAddress: &envoycore.SocketAddress{
							Protocol: envoycore.TCP,
							Address:  "1.2.3.4",
							PortSpecifier: &envoycore.SocketAddress_PortValue{
								PortValue: 1234,
							},
						},
					},
				},
			},
		},
	}

	cs := clusterService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			resources := make([]types.Any, len(expectedClusters))
			var typeURL string
			for idx, c := range expectedClusters {
				any, err := types.MarshalAny(c)
				if err != nil {
					return nil, err
				}

				resources[idx] = *any
				typeURL = any.GetTypeUrl()
			}

			return &envoyapi.DiscoveryResponse{
				Resources: resources,
				TypeUrl:   typeURL,
			}, nil
		},
	})

	res, err := fetchClusters(cs, mkEmptyRequest())
	assert.Nil(t, err)
	assert.NonNil(t, res)
	assert.ArrayEqual(t, res, expectedClusters)
}

func TestAsClusterResolverUnderlyingErrorsReturned(t *testing.T) {
	es := endpointService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			return nil, errors.New("boom")
		},
	})

	is, errs := asClusterResolver(es, mkEmptyRequest)("")
	assert.Equal(t, len(is), 0)
	assert.Equal(t, len(errs), 1)
}

func TestAsClusterResolverUnderlyingEmptyResponse(t *testing.T) {
	es := endpointService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			return &envoyapi.DiscoveryResponse{}, nil
		},
	})

	is, errs := asClusterResolver(es, mkEmptyRequest)("")
	assert.Equal(t, len(is), 0)
	assert.Equal(t, len(errs), 0)
}

func TestAsClusterResolverClusterLoadAssignmentEndpointsConverted(t *testing.T) {
	clusterName := "cluster-name"
	expectedInstances := api.Instances{
		{
			Host: "1.2.3.4",
			Port: 1234,
			Metadata: api.Metadata{
				{
					Key:   "stage",
					Value: "prod",
				},
				{
					Key:   "build",
					Value: "cookie_monster",
				},
				{
					Key:   "sw_version",
					Value: "93bf93b",
				},
			},
		},
		{
			Host: "1.2.3.5",
			Port: 1235,
			Metadata: api.Metadata{
				{
					Key:   "stage",
					Value: "staging",
				},
				{
					Key:   "build",
					Value: "good",
				},
				{
					Key:   "sw_version",
					Value: "alpha",
				},
			},
		},
	}

	cla := &envoyapi.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints: []envoyendpoint.LocalityLbEndpoints{
			{
				LbEndpoints: []envoyendpoint.LbEndpoint{
					{
						Endpoint: &envoyendpoint.Endpoint{
							Address: &envoycore.Address{
								Address: &envoycore.Address_SocketAddress{
									SocketAddress: &envoycore.SocketAddress{
										Protocol: envoycore.TCP,
										Address:  "1.2.3.4",
										PortSpecifier: &envoycore.SocketAddress_PortValue{
											PortValue: 1234,
										},
									},
								},
							},
						},
						HealthStatus: envoycore.HealthStatus_HEALTHY,
						Metadata: &envoycore.Metadata{
							FilterMetadata: map[string]*types.Struct{
								"envoy.lb": {
									Fields: map[string]*types.Value{
										"stage": {
											Kind: &types.Value_StringValue{
												StringValue: "prod",
											},
										},
										"build": {
											Kind: &types.Value_StringValue{
												StringValue: "cookie_monster",
											},
										},
										"sw_version": {
											Kind: &types.Value_StringValue{
												StringValue: "93bf93b",
											},
										},
									},
								},
							},
						},
					},
					{
						Endpoint: &envoyendpoint.Endpoint{
							Address: &envoycore.Address{
								Address: &envoycore.Address_SocketAddress{
									SocketAddress: &envoycore.SocketAddress{
										Protocol: envoycore.TCP,
										Address:  "1.2.3.5",
										PortSpecifier: &envoycore.SocketAddress_PortValue{
											PortValue: 1235,
										},
									},
								},
							},
						},
						HealthStatus: envoycore.HealthStatus_HEALTHY,
						Metadata: &envoycore.Metadata{
							FilterMetadata: map[string]*types.Struct{
								"envoy.lb": {
									Fields: map[string]*types.Value{
										"stage": {
											Kind: &types.Value_StringValue{
												StringValue: "staging",
											},
										},
										"build": {
											Kind: &types.Value_StringValue{
												StringValue: "good",
											},
										},
										"sw_version": {
											Kind: &types.Value_StringValue{
												StringValue: "alpha",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	any, err := types.MarshalAny(cla)
	assert.Nil(t, err)
	response := &envoyapi.DiscoveryResponse{
		Resources: []types.Any{*any},
	}

	es := endpointService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			return response, nil
		},
	})

	is, errs := asClusterResolver(es, mkEmptyRequest)(clusterName)
	assert.Equal(t, len(is), len(expectedInstances))
	for idx := range is {
		assert.Equal(t, is[idx].Host, expectedInstances[idx].Host)
		assert.Equal(t, is[idx].Port, expectedInstances[idx].Port)
		assert.HasSameElements(t, is[idx].Metadata, expectedInstances[idx].Metadata)
	}

	assert.Equal(t, len(errs), 0)
}

func TestEndpointServiceSourceEmptyRequestReturnsUnderlyingResult(t *testing.T) {
	fetchCallCount := 0
	expectedClusterName := "expected"

	es := endpointService(&fnDiscoveryService{
		fetchFn: func(r *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			fetchCallCount++
			assert.Equal(t, len(r.ResourceNames), 1)
			assert.Equal(t, r.ResourceNames[0], expectedClusterName)

			return &envoyapi.DiscoveryResponse{}, nil
		},
	})

	rf := func() *envoyapi.DiscoveryRequest {
		return nil
	}

	res, err := asClusterResolver(es, rf)(expectedClusterName)
	assert.Nil(t, res)
	assert.Nil(t, err)
	assert.Equal(t, fetchCallCount, 1)
}

func TestEndpointServiceSourceUnderlyingErrorPropagated(t *testing.T) {
	fetchCallCount := 0
	es := endpointService(&fnDiscoveryService{
		fetchFn: func(*envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			fetchCallCount++
			return nil, errors.New("boom")
		},
	})

	res, err := asClusterResolver(es, mkEmptyRequest)("")
	assert.Nil(t, res)
	assert.NonNil(t, err)
	assert.Equal(t, fetchCallCount, 1)
}

func TestEndpointServiceSourceTooManyResponsesReturnsError(t *testing.T) {
	clusterName := "cluster-name"
	es := endpointService(&fnDiscoveryService{
		fetchFn: func(dr *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			assert.ArrayEqual(t, dr.GetResourceNames(), []string{clusterName})

			return &envoyapi.DiscoveryResponse{
				Resources: []types.Any{
					{
						TypeUrl: "blerp",
					},
					{
						TypeUrl: "blap",
					},
				},
			}, nil
		},
	})

	res, err := asClusterResolver(es, mkEmptyRequest)(clusterName)
	assert.Nil(t, res)
	assert.NonNil(t, err)
}

func TestEndpointServiceSourceBadUnmarshalReturnsError(t *testing.T) {
	clusterName := "cluster-name"
	es := endpointService(&fnDiscoveryService{
		fetchFn: func(dr *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
			assert.ArrayEqual(t, dr.GetResourceNames(), []string{clusterName})

			return &envoyapi.DiscoveryResponse{
				Resources: []types.Any{
					{
						TypeUrl: "invalid-type-url",
					},
				},
			}, nil
		},
	})

	res, err := asClusterResolver(es, mkEmptyRequest)(clusterName)
	assert.Nil(t, res)
	assert.NonNil(t, err)
}
