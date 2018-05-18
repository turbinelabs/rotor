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
	"net"
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
)

func checkClusterLoadAssignments(
	t *testing.T,
	actual cache.Resources,
	expected []envoyapi.ClusterLoadAssignment,
) {
	assert.Equal(t, len(actual.Items), len(expected))

	actualLoadAssignment := []envoyapi.ClusterLoadAssignment{}
	for _, resource := range actual.Items {
		if loadAssignment, ok := resource.(*envoyapi.ClusterLoadAssignment); !ok {
			assert.Failed(t, "could not cast resource to ClusterLoadAssignment")
		} else {
			actualLoadAssignment = append(actualLoadAssignment, *loadAssignment)
		}
	}

	assert.HasSameElements(t, actualLoadAssignment, expected)
}

func TestEmptyRequestNoClusterLoadAssignments(t *testing.T) {
	objects := poller.MkFixtureObjects()
	objects.Clusters = nil

	resources, err := eds{}.edsResourceAdapter(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.Equal(t, len(resources.Items), 0)
}

func TestFullRequestFullClusterLoadAssignments(t *testing.T) {
	objects := poller.MkFixtureObjects()
	objects.Clusters[0].Instances[0].Host = "localhost"
	resolveDNS := func(host string) ([]net.IP, error) {
		if host == "localhost" {
			return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}, nil
		}
		return []net.IP{net.ParseIP(host)}, nil
	}

	resources, err := eds{resolveDNS}.edsResourceAdapter(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.NonNil(t, resources.Items)

	expectedClusterAssignments := []envoyapi.ClusterLoadAssignment{
		{
			ClusterName: "foo",
			Endpoints: []envoyendpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []envoyendpoint.LbEndpoint{
						{
							Endpoint: &envoyendpoint.Endpoint{
								Address: &envoycore.Address{
									Address: &envoycore.Address_SocketAddress{
										SocketAddress: &envoycore.SocketAddress{
											Protocol: envoycore.TCP,
											Address:  "1.1.1.1",
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
											"stage":      valueString("prod"),
											"build":      valueString("cookie_monster"),
											"sw_version": valueString("93bf93b"),
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
											Address:  "2.2.2.2",
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
											"stage":      valueString("prod"),
											"build":      valueString("cookie_monster"),
											"sw_version": valueString("93bf93b"),
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
											"stage":      valueString("canary"),
											"build":      valueString("kermit"),
											"sw_version": valueString("93bf93b"),
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
											Address:  "1.2.3.6",
											PortSpecifier: &envoycore.SocketAddress_PortValue{
												PortValue: 1236,
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
											"stage":      valueString("dev"),
											"sw_version": valueString("9d323ec"),
											"build":      valueString("cookie_monster"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			ClusterName: "baz",
			Endpoints: []envoyendpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []envoyendpoint.LbEndpoint{
						{
							Endpoint: &envoyendpoint.Endpoint{
								Address: &envoycore.Address{
									Address: &envoycore.Address_SocketAddress{
										SocketAddress: &envoycore.SocketAddress{
											Protocol: envoycore.TCP,
											Address:  "1.2.4.8",
											PortSpecifier: &envoycore.SocketAddress_PortValue{
												PortValue: 8800,
											},
										},
									},
								},
							},
							HealthStatus: envoycore.HealthStatus_HEALTHY,
							Metadata:     &envoycore.Metadata{},
						},
					},
				},
			},
		},
		{
			ClusterName: "bar",
			Endpoints: []envoyendpoint.LocalityLbEndpoints{
				{
					LbEndpoints: []envoyendpoint.LbEndpoint{
						{
							Endpoint: &envoyendpoint.Endpoint{
								Address: &envoycore.Address{
									Address: &envoycore.Address_SocketAddress{
										SocketAddress: &envoycore.SocketAddress{
											Protocol: envoycore.TCP,
											Address:  "1.2.3.7",
											PortSpecifier: &envoycore.SocketAddress_PortValue{
												PortValue: 1237,
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
											"stage": valueString("prod"),
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
											Address:  "1.2.3.8",
											PortSpecifier: &envoycore.SocketAddress_PortValue{
												PortValue: 1238,
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
											"stage":      valueString("prod"),
											"build":      valueString("kermit"),
											"sw_version": valueString("93bf93b"),
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
											Address:  "1.2.3.9",
											PortSpecifier: &envoycore.SocketAddress_PortValue{
												PortValue: 1239,
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
											"stage":      valueString("dev"),
											"build":      valueString("cookie_monster"),
											"sw_version": valueString("9d323ec"),
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

	checkClusterLoadAssignments(t, resources, expectedClusterAssignments)
}

func compareMetadata(t *testing.T, tbnMeta map[string]string, envoyMeta map[string]*types.Struct) {
	pStruct := envoyMeta[envoyLb]
	assert.NonNil(t, pStruct)
	assert.Equal(t, len(tbnMeta), len(pStruct.GetFields()))
	for key, value := range pStruct.GetFields() {
		assert.Equal(t, tbnMeta[key], value.GetStringValue())
	}
}

func headSelector(is tbnapi.Instances) tbnapi.Instance {
	if len(is) == 0 {
		return tbnapi.Instance{}
	}

	return is[0]
}

func TestEnvoyEndpointsToTbnInstancesEmptyInputReturnsEmpty(t *testing.T) {
	o, errs := envoyEndpointsToTbnInstances(nil)
	assert.Equal(t, len(o), 0)
	assert.Equal(t, len(errs), 0)
}

func TestEnvoyEndpointsToTbnInstancesReturnsErrorsAndGoodInstances(t *testing.T) {
	lles := []envoyendpoint.LocalityLbEndpoints{
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
					Metadata: &envoycore.Metadata{
						FilterMetadata: map[string]*types.Struct{
							"envoy.lb": {
								Fields: map[string]*types.Value{
									"f1": {
										Kind: &types.Value_StringValue{
											StringValue: "v1",
										},
									},
									"f2": {
										Kind: &types.Value_StringValue{
											StringValue: "v2",
										},
									},
								},
							},
						},
					},
				},
				{
					Endpoint: &envoyendpoint.Endpoint{},
					Metadata: &envoycore.Metadata{},
				},
			},
		},
	}

	is, errs := envoyEndpointsToTbnInstances(lles)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "Error making instances for endpoint")
	assert.Equal(t, len(is), 1)

	instance := is[0]
	lbEndpoint := lles[0].GetLbEndpoints()[0]
	addr := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()
	assert.Equal(t, instance.Host, addr.GetAddress())
	assert.Equal(t, instance.Port, int(addr.GetPortValue()))
	compareMetadata(
		t,
		instance.Metadata.Map(),
		lbEndpoint.GetMetadata().GetFilterMetadata(),
	)
}

func TestEnvoyEndpointsToTbnInstancesReturnsInstancesWithMetadata(t *testing.T) {
	lles := []envoyendpoint.LocalityLbEndpoints{
		{
			Locality: &envoycore.Locality{},
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
					Metadata: &envoycore.Metadata{
						FilterMetadata: map[string]*types.Struct{
							"envoy.lb": {
								Fields: map[string]*types.Value{
									"f1": {
										Kind: &types.Value_StringValue{
											StringValue: "v1",
										},
									},
									"f2": {
										Kind: &types.Value_StringValue{
											StringValue: "v2",
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
					Metadata: &envoycore.Metadata{
						FilterMetadata: map[string]*types.Struct{
							"envoy.lb": {
								Fields: map[string]*types.Value{
									"f1": {
										Kind: &types.Value_StringValue{
											StringValue: "v1",
										},
									},
									"f2": {
										Kind: &types.Value_StringValue{
											StringValue: "v2",
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

	is, errs := envoyEndpointsToTbnInstances(lles)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, len(is), len(lles[0].GetLbEndpoints()))
	for idx := range is {
		lbEndpoint := lles[0].GetLbEndpoints()[idx]
		addr := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()
		assert.Equal(t, is[idx].Host, addr.GetAddress())
		assert.Equal(t, is[idx].Port, int(addr.GetPortValue()))

		compareMetadata(
			t,
			is[idx].Metadata.Map(),
			lbEndpoint.GetMetadata().GetFilterMetadata(),
		)
	}
}

func TestEnvoyEndpointToTbnInstanceReturnsErrorForEmptyLbEndpoint(t *testing.T) {
	badInputs := []envoyendpoint.LbEndpoint{
		{},
		{Endpoint: &envoyendpoint.Endpoint{}},
	}

	for _, badInput := range badInputs {
		i, err := envoyEndpointToTbnInstance(badInput)
		assert.Nil(t, i)
		assert.ErrorContains(t, err, "Cannot convert empty Address")
	}
}

func TestEnvoyAddrToTbnInstanceErrorReturned(t *testing.T) {
	input := &envoycore.Address{
		Address: &envoycore.Address_Pipe{
			Pipe: &envoycore.Pipe{},
		},
	}

	i, err := envoyAddrToTbnInstance(input, nil)
	assert.Nil(t, i)
	assert.ErrorContains(t, err, "Unsupported address type")
}

func TestEnvoyAddrToHostPortErrorOnNonSocketAddress(t *testing.T) {
	addr := &envoycore.Address{
		Address: &envoycore.Address_Pipe{
			Pipe: &envoycore.Pipe{},
		},
	}

	h, p, err := envoyAddrToHostPort(addr)
	assert.Equal(t, h, "")
	assert.Equal(t, p, uint32(0))
	assert.ErrorContains(t, err, "Unsupported address type")
}

func TestEnvoyAddrToHostPortErrorOnEmptyAddress(t *testing.T) {
	addr := &envoycore.Address{
		Address: &envoycore.Address_SocketAddress{
			SocketAddress: &envoycore.SocketAddress{},
		},
	}

	h, p, err := envoyAddrToHostPort(addr)
	assert.Equal(t, h, "")
	assert.Equal(t, p, uint32(0))
	assert.ErrorContains(t, err, "Invalid host for Address")
}

func TestEnvoyAddrToHostPortErrorOnInvalidPortValue(t *testing.T) {
	addr := &envoycore.Address{
		Address: &envoycore.Address_SocketAddress{
			SocketAddress: &envoycore.SocketAddress{
				Address:       "1.2.3.4",
				PortSpecifier: &envoycore.SocketAddress_PortValue{},
			},
		},
	}

	h, p, err := envoyAddrToHostPort(addr)
	assert.Equal(t, h, "")
	assert.Equal(t, p, uint32(0))
	assert.ErrorContains(t, err, "Invalid port for Address")
}

func TestEnvoyAddrToHostPortReturnsSuccessfulParse(t *testing.T) {
	addr := &envoycore.Address{
		Address: &envoycore.Address_SocketAddress{
			SocketAddress: &envoycore.SocketAddress{
				Address: "1.2.3.4",
				PortSpecifier: &envoycore.SocketAddress_PortValue{
					PortValue: uint32(1234),
				},
			},
		},
	}

	h, p, err := envoyAddrToHostPort(addr)
	assert.Nil(t, err)
	assert.Equal(t, h, "1.2.3.4")
	assert.Equal(t, p, uint32(1234))
}

func TestEnvoyMetadataToTbnMetadataReturnsEmptyOnEmptyInput(t *testing.T) {
	m := envoyMetadataToTbnMetadata(nil)
	assert.Equal(t, len(m), 0)

	m = envoyMetadataToTbnMetadata(&envoycore.Metadata{})
	assert.Equal(t, len(m), 0)
}

func TestEnvoyMetadataToTbnMetadataReturnsEmptyOnPopulatedMetadataWithDifferentKeys(t *testing.T) {
	m := envoyMetadataToTbnMetadata(&envoycore.Metadata{
		FilterMetadata: map[string]*types.Struct{
			"not-envoy.lb": {
				Fields: map[string]*types.Value{
					"field1": {
						Kind: &types.Value_StringValue{
							StringValue: "value1",
						},
					},
				},
			},
		},
	})

	assert.Equal(t, len(m), 0)
}

func TestEnvoyMetadataToTbnMetadataProperlyConvertsInputIgnoresStructsLists(t *testing.T) {
	m := envoyMetadataToTbnMetadata(&envoycore.Metadata{
		FilterMetadata: map[string]*types.Struct{
			"envoy.lb": {
				Fields: map[string]*types.Value{
					"field1": {
						Kind: &types.Value_StringValue{
							StringValue: "value1",
						},
					},
					"field2": {
						Kind: &types.Value_NullValue{},
					},
					"field3": {
						Kind: &types.Value_NumberValue{
							NumberValue: float64(1.43234234),
						},
					},
					"field4": {
						Kind: &types.Value_BoolValue{
							BoolValue: true,
						},
					},
					"field5": {
						Kind: &types.Value_StructValue{
							StructValue: &types.Struct{},
						},
					},
					"field6": {
						Kind: &types.Value_ListValue{
							ListValue: &types.ListValue{},
						},
					},
				},
			},
		},
	})

	assert.Equal(t, len(m), 4)
	mm := m.Map()
	assert.Equal(t, mm["field1"], "value1")
	assert.Equal(t, mm["field2"], "")
	assert.Equal(t, mm["field3"], "1.43234234")
	assert.Equal(t, mm["field4"], "true")
}
