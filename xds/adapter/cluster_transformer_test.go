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
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoycluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/collector"
	"github.com/turbinelabs/test/assert"
)

// Utility to help test fnClusterTransformer. Keeps track of clusters it's been
// called with and provides methods to verify.
type mockUnderlyingTransformer struct {
	staticClusterNames  []string
	expectedStaticErrs  []error
	dynamicClusterNames []string
	expectedDynamicErrs []error
}

func (p *mockUnderlyingTransformer) staticTransform(a *envoyapi.Cluster) (*api.Cluster, []error) {
	if a == nil {
		return nil, p.expectedStaticErrs
	}

	p.staticClusterNames = append(p.staticClusterNames, a.GetName())
	return &api.Cluster{Name: a.GetName()}, p.expectedStaticErrs
}

func (p *mockUnderlyingTransformer) dynamicTransform(
	a *envoyapi.Cluster,
	m map[string]*envoyapi.Cluster,
) (*api.Cluster, []error) {
	if a == nil {
		return nil, p.expectedDynamicErrs
	}

	p.dynamicClusterNames = append(p.dynamicClusterNames, a.GetName())

	return &api.Cluster{Name: a.GetName()}, p.expectedDynamicErrs
}

func (p *mockUnderlyingTransformer) verifyStaticTransformCalls(t *testing.T, names []string) {
	assert.HasSameElements(t, p.staticClusterNames, names)
}

func (p *mockUnderlyingTransformer) verifyDynamicTransformCalls(t *testing.T, names []string) {
	assert.HasSameElements(t, p.dynamicClusterNames, names)
}

func mkMockedTransformer(
	expectedStaticErrs []error,
	expectedDynamicErrs []error,
) (*fnClusterTransformer, *mockUnderlyingTransformer) {
	mut := &mockUnderlyingTransformer{
		expectedStaticErrs:  expectedStaticErrs,
		expectedDynamicErrs: expectedDynamicErrs,
	}

	ct := &fnClusterTransformer{
		staticTransform:  mut.staticTransform,
		dynamicTransform: mut.dynamicTransform,
	}

	return ct, mut
}

func TestClusterTransformerInvalidClusterTypeSkipped(t *testing.T) {
	clusterName := "bad-cluster"
	input := []*envoyapi.Cluster{
		{
			Name: clusterName,
			Type: envoyapi.Cluster_ORIGINAL_DST,
		},
	}

	ct, mut := mkMockedTransformer(nil, nil)
	cs, errMap := ct.transform(input)
	assert.Equal(t, len(cs), 0)
	assert.Equal(t, len(errMap), 1)
	assert.Equal(t, len(errMap[clusterName]), 1)
	mut.verifyStaticTransformCalls(t, nil)
	mut.verifyDynamicTransformCalls(t, nil)
}

func TestClusterTransformerGroupsClustersAccordingly(t *testing.T) {
	staticInput := []*envoyapi.Cluster{
		{
			Name: "static-1",
			Type: envoyapi.Cluster_STATIC,
		},
		{
			Name: "static-2",
			Type: envoyapi.Cluster_STRICT_DNS,
		},
		{
			Name: "static-3",
			Type: envoyapi.Cluster_LOGICAL_DNS,
		},
	}

	dynamicInput := []*envoyapi.Cluster{
		{
			Name: "dynamic-1",
			Type: envoyapi.Cluster_EDS,
		},
		{
			Name: "dynamic-2",
			Type: envoyapi.Cluster_EDS,
		},
	}
	input := append(staticInput, dynamicInput...)

	ct, mut := mkMockedTransformer(nil, nil)
	cs, errMap := ct.transform(input)
	assert.Equal(t, len(cs), len(input))
	assert.Equal(t, len(errMap), 0)
	mut.verifyStaticTransformCalls(t, []string{"static-1", "static-2", "static-3"})
	mut.verifyDynamicTransformCalls(t, []string{"dynamic-1", "dynamic-2"})
}

func TestClusterTransformerNilClustersSkipped(t *testing.T) {
	input := []*envoyapi.Cluster{nil, nil, nil}
	ct, mut := mkMockedTransformer(nil, nil)
	cs, errMap := ct.transform(input)
	assert.Equal(t, len(cs), 0)
	assert.Equal(t, len(errMap), 0)
	mut.verifyStaticTransformCalls(t, nil)
	mut.verifyDynamicTransformCalls(t, nil)
}

func TestClusterTransformerUnderlyingErrorsGetReturned(t *testing.T) {
	errs := []error{errors.New("boom")}
	staticInput := []*envoyapi.Cluster{
		{
			Name: "cluster-1",
			Type: envoyapi.Cluster_LOGICAL_DNS,
		},
		{
			Name: "cluster-2",
			Type: envoyapi.Cluster_STATIC,
		},
	}
	dynamicInput := []*envoyapi.Cluster{
		{
			Name: "cluster-3",
			Type: envoyapi.Cluster_EDS,
		},
		{
			Name: "cluster-4",
			Type: envoyapi.Cluster_EDS,
		},
	}
	input := append(staticInput, dynamicInput...)
	ct, mut := mkMockedTransformer(errs, errs)

	cs, errMap := ct.transform(input)
	assert.Equal(t, len(cs), len(input))
	assert.Equal(t, len(errMap), len(input))
	for _, ic := range input {
		assert.ArrayEqual(t, errMap[ic.GetName()], errs)
	}

	mut.verifyStaticTransformCalls(t, []string{"cluster-1", "cluster-2"})
	mut.verifyDynamicTransformCalls(t, []string{"cluster-3", "cluster-4"})
}

func TestMkStaticClusterNoName(t *testing.T) {
	c, errs := mkStaticCluster(nil)
	assert.Nil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "Empty cluster name returned")
}

func TestMkStaticClusterNoHostsNoTlsContextNoCircuitBreakers(t *testing.T) {
	i := &envoyapi.Cluster{Name: "c1"}
	o, errs := mkStaticCluster(i)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, o.Name, i.GetName())
	assert.False(t, o.RequireTLS)
	assert.Equal(t, len(o.Instances), 0)
}

func TestMkStaticClustersNoHostsTlsContextNoCircuitBreakers(t *testing.T) {
	i := &envoyapi.Cluster{
		Name:       "c1",
		TlsContext: &envoyauth.UpstreamTlsContext{},
	}
	o, errs := mkStaticCluster(i)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, o.Name, i.GetName())
	assert.True(t, o.RequireTLS)
	assert.Equal(t, len(o.Instances), 0)
}

func TestMkStaticClustersNoHostsTlsContextWithCircuitBreakers(t *testing.T) {
	i := &envoyapi.Cluster{
		Name:       "c1",
		TlsContext: &envoyauth.UpstreamTlsContext{},
		CircuitBreakers: &envoycluster.CircuitBreakers{
			Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
				{
					Priority:   envoycore.RoutingPriority_DEFAULT,
					MaxRetries: &types.UInt32Value{Value: uint32(10)},
				},
			},
		},
	}

	o, errs := mkStaticCluster(i)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, o.Name, i.GetName())
	assert.True(t, o.RequireTLS)
	assert.Equal(t, len(o.Instances), 0)
	assert.Equal(t, *o.CircuitBreakers.MaxRetries, 10)
}

func TestMkStaticClustersWithHostsAndCircuitBreakers(t *testing.T) {
	i := &envoyapi.Cluster{
		Name:       "c1",
		Type:       envoyapi.Cluster_STATIC,
		TlsContext: &envoyauth.UpstreamTlsContext{},
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
			{
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
		CircuitBreakers: &envoycluster.CircuitBreakers{
			Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
				{
					Priority:           envoycore.RoutingPriority_DEFAULT,
					MaxConnections:     &types.UInt32Value{Value: uint32(10)},
					MaxRetries:         &types.UInt32Value{Value: uint32(20)},
					MaxPendingRequests: &types.UInt32Value{Value: uint32(30)},
					MaxRequests:        &types.UInt32Value{Value: uint32(40)},
				},
			},
		},
	}

	o, errs := mkStaticCluster(i)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, o.Name, i.GetName())
	assert.True(t, o.RequireTLS)
	assert.Equal(t, len(o.Instances), len(i.Hosts))
	for idx := range i.Hosts {
		actual := o.Instances[idx]
		expected := i.GetHosts()[idx]
		assert.Equal(t, actual.Host, expected.GetSocketAddress().GetAddress())
		assert.Equal(t, actual.Port, int(expected.GetSocketAddress().GetPortValue()))
	}

	assert.Equal(t, *o.CircuitBreakers.MaxConnections, 10)
	assert.Equal(t, *o.CircuitBreakers.MaxRetries, 20)
	assert.Equal(t, *o.CircuitBreakers.MaxPendingRequests, 30)
	assert.Equal(t, *o.CircuitBreakers.MaxRequests, 40)
}

func TestMkStaticClustersBadHostsGetSkipped(t *testing.T) {
	i := &envoyapi.Cluster{
		Name:       "c1",
		Type:       envoyapi.Cluster_STATIC,
		TlsContext: &envoyauth.UpstreamTlsContext{},
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
			{
				Address: &envoycore.Address_Pipe{},
			},
		},
		CircuitBreakers: &envoycluster.CircuitBreakers{
			Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
				{
					Priority:    envoycore.RoutingPriority_DEFAULT,
					MaxRequests: &types.UInt32Value{Value: uint32(40)},
				},
			},
		},
	}

	o, errs := mkStaticCluster(i)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "Error creating Instance")
	assert.Equal(t, o.Name, i.GetName())
	assert.True(t, o.RequireTLS)
	assert.Equal(t, len(o.Instances), 1)
	assert.Equal(t, o.Instances[0].Host, i.Hosts[0].GetSocketAddress().GetAddress())
	assert.Equal(t, o.Instances[0].Port, int(i.Hosts[0].GetSocketAddress().GetPortValue()))
	assert.Equal(t, *o.CircuitBreakers.MaxRequests, 40)
}

func TestMkDynamicClusterEmptyNameError(t *testing.T) {
	c, errs := mkDynamicCluster(&envoyapi.Cluster{}, nil)
	assert.Nil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "Empty cluster name")
}

func TestMkDynamicClusterEmptyEdsClusterConfigError(t *testing.T) {
	c, errs := mkDynamicCluster(&envoyapi.Cluster{Name: "c1"}, nil)
	assert.Nil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "No EdsClusterConfig")
}

func TestMkDynamicClusterEmptyEdsConfigError(t *testing.T) {
	i := &envoyapi.Cluster{Name: "c1", EdsClusterConfig: &envoyapi.Cluster_EdsClusterConfig{}}
	c, errs := mkDynamicCluster(i, nil)
	assert.Nil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "No EdsClusterConfig")
}

func TestMkDynamicClusterMkClusterResolverErrorReturned(t *testing.T) {
	expectedConfigSource := &envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_Path{Path: "/path"},
	}
	i := &envoyapi.Cluster{
		Name: "c1",
		EdsClusterConfig: &envoyapi.Cluster_EdsClusterConfig{
			EdsConfig: expectedConfigSource,
		},
	}

	mkResolver := func(cs *envoycore.ConfigSource) (collector.ClusterResolver, error) {
		assert.Equal(t, cs, expectedConfigSource)
		return nil, errors.New("boom")
	}

	c, errs := mkDynamicCluster(i, mkResolver)
	assert.Nil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "boom")
}

func TestMkDynamicClusterMkClusterResolverWithErrors(t *testing.T) {
	expectedConfigSource := &envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_Path{Path: "/path"},
	}
	i := &envoyapi.Cluster{
		Name: "c1",
		EdsClusterConfig: &envoyapi.Cluster_EdsClusterConfig{
			EdsConfig: expectedConfigSource,
		},
		CircuitBreakers: &envoycluster.CircuitBreakers{
			Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
				{
					Priority:    envoycore.RoutingPriority_DEFAULT,
					MaxRequests: &types.UInt32Value{Value: uint32(40)},
				},
			},
		},
	}

	instances := api.Instances{
		{
			Host: "host1",
			Port: 1,
		},
		{
			Host: "host2",
			Port: 2,
		},
	}

	mkResolver := func(cs *envoycore.ConfigSource) (collector.ClusterResolver, error) {
		assert.Equal(t, cs, expectedConfigSource)

		return func(string) (api.Instances, []error) {
			return instances, nil
		}, errors.New("boom")
	}

	c, errs := mkDynamicCluster(i, mkResolver)
	assert.NonNil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "boom")
	assert.Equal(t, c.Name, i.GetName())
	assert.False(t, c.RequireTLS)
	assert.ArrayEqual(t, c.Instances, instances)
	assert.Equal(t, *c.CircuitBreakers.MaxRequests, 40)
}

func TestMkDynamicClusterClusterResolverReturnsErrors(t *testing.T) {
	expectedConfigSource := &envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_Path{Path: "/path"},
	}

	i := &envoyapi.Cluster{
		Name: "c1",
		EdsClusterConfig: &envoyapi.Cluster_EdsClusterConfig{
			EdsConfig: expectedConfigSource,
		},
	}

	instances := api.Instances{
		{
			Host: "host1",
			Port: 1,
		},
		{
			Host: "host2",
			Port: 2,
		},
	}

	mkResolver := func(cs *envoycore.ConfigSource) (collector.ClusterResolver, error) {
		assert.Equal(t, cs, expectedConfigSource)

		return func(string) (api.Instances, []error) {
			return instances, []error{errors.New("boom")}
		}, nil
	}

	c, errs := mkDynamicCluster(i, mkResolver)
	assert.NonNil(t, c)
	assert.Equal(t, len(errs), 1)
	assert.ErrorContains(t, errs[0], "boom")
	assert.Equal(t, c.Name, i.GetName())
	assert.False(t, c.RequireTLS)
	assert.ArrayEqual(t, c.Instances, instances)
}

func TestNewClusterResolverNonApiConfigError(t *testing.T) {
	badConfigSource := &envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_Path{Path: "/path"},
	}

	cr, err := newClusterResolver(badConfigSource, nil, nil, nil)
	assert.Nil(t, cr)
	assert.ErrorContains(t, err, "Only ApiConfigSource supported")
}

func newClusterResolverForAPIType(
	apiType envoycore.ApiConfigSource_ApiType,
) (collector.ClusterResolver, error) {
	scMap := map[string]*envoyapi.Cluster{
		"c1": {
			Name: "c1",
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
				{
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
		},
	}

	cs := &envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_ApiConfigSource{
			ApiConfigSource: &envoycore.ApiConfigSource{
				ApiType:      apiType,
				ClusterNames: []string{"c1"},
			},
		},
	}

	return newClusterResolver(cs, mkEmptyRequest, scMap, headSelector)
}

func TestNewClusterResolverForRestLegacy(t *testing.T) {
	cr, err := newClusterResolverForAPIType(envoycore.ApiConfigSource_REST_LEGACY)
	assert.NonNil(t, cr)
	assert.Nil(t, err)
}

func TestNewClusterResolverForRest(t *testing.T) {
	cr, err := newClusterResolverForAPIType(envoycore.ApiConfigSource_REST)
	assert.NonNil(t, cr)
	assert.Nil(t, err)
}

func TestNewClusterResolverForGrpc(t *testing.T) {
	cr, err := newClusterResolverForAPIType(envoycore.ApiConfigSource_GRPC)
	assert.NonNil(t, cr)
	assert.Nil(t, err)
}

func TestNewClusterResolverForUnknownApiType(t *testing.T) {
	cr, err := newClusterResolverForAPIType(envoycore.ApiConfigSource_ApiType(5))
	assert.Nil(t, cr)
	assert.ErrorContains(t, err, "Unrecognized ApiConfigSourceType")
}

func TestResolveEDSInstanceReturnsEmptyForEmptyClusterNames(t *testing.T) {
	i, err := resolveEDSInstance([]string{}, headSelector, nil)
	assert.Equal(t, i.Host, "")
	assert.Equal(t, i.Port, 0)
	assert.Equal(t, len(i.Metadata), 0)
	assert.ErrorContains(t, err, "No EDS hosts resolved for cluster")
}

func TestResolveEDSInstanceReturnsEmptyForEmptyStaticHosts(t *testing.T) {
	i, err := resolveEDSInstance([]string{"c1", "c2"}, headSelector, nil)
	assert.Equal(t, i.Host, "")
	assert.Equal(t, i.Port, 0)
	assert.Equal(t, len(i.Metadata), 0)
	assert.ErrorContains(t, err, "No EDS hosts resolved for cluster")
}

func TestResolveEDSInstanceReturnsEmptyForUnknownStaticHosts(t *testing.T) {
	m := map[string]*envoyapi.Cluster{
		"c1": {},
		"c2": {},
	}
	i, err := resolveEDSInstance([]string{"c3", "c4"}, headSelector, m)
	assert.Equal(t, i.Host, "")
	assert.Equal(t, i.Port, 0)
	assert.Equal(t, len(i.Metadata), 0)
	assert.ErrorContains(t, err, "No EDS hosts resolved for cluster")
}

func TestResolveEDSInstanceReturnsErrorForBadInstanceAddress(t *testing.T) {
	m := map[string]*envoyapi.Cluster{
		"c1": {
			Name: "c1",
			Hosts: []*envoycore.Address{
				{
					Address: &envoycore.Address_Pipe{
						Pipe: &envoycore.Pipe{},
					},
				},
			},
		},
	}

	i, err := resolveEDSInstance([]string{"c1"}, headSelector, m)
	assert.Equal(t, i.Host, "")
	assert.Equal(t, i.Port, 0)
	assert.Equal(t, len(i.Metadata), 0)
	assert.ErrorContains(t, err, "Unsupported address type")
}

func TestResolveEDSInstanceReturnsGoodInstance(t *testing.T) {
	m := map[string]*envoyapi.Cluster{
		"c1": {
			Name: "c1",
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
				{
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
		},
	}

	i, err := resolveEDSInstance([]string{"c1"}, headSelector, m)
	assert.Nil(t, err)
	assert.Equal(t, i.Host, "1.2.3.4")
	assert.Equal(t, i.Port, 1234)
	assert.Equal(t, len(i.Metadata), 0)
}
