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
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoylog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	envoyhcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/rotor/xds/log"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
)

func checkListeners(
	t *testing.T,
	actual cache.Resources,
	expected []envoyapi.Listener,
) {
	assert.Equal(t, len(actual.Items), len(expected))

	actualEnvoyListeners := []envoyapi.Listener{}
	for _, resource := range actual.Items {
		if listener, ok := resource.(*envoyapi.Listener); !ok {
			assert.Failed(t, "could not cast resource to listener")
		} else {
			actualEnvoyListeners = append(actualEnvoyListeners, *listener)
		}
	}

	assert.HasSameElements(t, actualEnvoyListeners, expected)
}

func TestMkHttpConnectionManager(t *testing.T) {
	s := lds{}

	httpConnMgr, err := s.mkHTTPConnectionManager("foo", 1000)
	assert.Nil(t, err)

	assert.DeepEqual(t,
		httpConnMgr,
		&envoyhcm.HttpConnectionManager{
			CodecType: envoyhcm.AUTO,
			// ":" is no-bueno for stats strings, other stuff gets properly escaped
			StatPrefix: "foo-1000",
			HttpFilters: []*envoyhcm.HttpFilter{
				{
					Name:   util.CORS,
					Config: &types.Struct{},
				},
				{
					Name: util.Router,
				},
			},
			RouteSpecifier: &envoyhcm.HttpConnectionManager_Rds{
				Rds: &envoyhcm.Rds{
					RouteConfigName: "foo:1000",
					ConfigSource:    xdsClusterConfig,
				},
			},
		},
	)
}

func TestMkHttpConnectionManagerWithGRPCLog(t *testing.T) {
	s := lds{loggingCluster: "log-cluster"}

	httpConnMgr, err := s.mkHTTPConnectionManager("foo", 1000)
	assert.Nil(t, err)

	assert.DeepEqual(t,
		httpConnMgr,
		&envoyhcm.HttpConnectionManager{
			CodecType: envoyhcm.AUTO,
			// ":" is no-bueno for stats strings, other stuff gets properly escaped
			StatPrefix: "foo-1000",
			HttpFilters: []*envoyhcm.HttpFilter{
				{
					Name:   util.CORS,
					Config: &types.Struct{},
				},
				{
					Name: util.Router,
					Config: mapToStruct(t, map[string]interface{}{
						"upstreamLog": []map[string]interface{}{
							{
								"name": util.HTTPGRPCAccessLog,
								"config": map[string]interface{}{
									"commonConfig": map[string]interface{}{
										"logName": grpcUpstreamLogID,
										"grpcService": map[string]interface{}{
											"envoyGrpc": map[string]interface{}{
												"clusterName": "log-cluster",
											},
										},
									},
									"additionalRequestHeadersToLog": log.EnvoyGRPCRequestHeaders(
										log.TbnUpstreamFormat,
									),
								},
							},
						},
					}),
				},
			},
			RouteSpecifier: &envoyhcm.HttpConnectionManager_Rds{
				Rds: &envoyhcm.Rds{
					RouteConfigName: "foo:1000",
					ConfigSource:    xdsClusterConfig,
				},
			},
			AccessLog: []*envoylog.AccessLog{
				{
					Name: util.HTTPGRPCAccessLog,
					Config: mapToStruct(t, map[string]interface{}{
						"commonConfig": map[string]interface{}{
							"logName": grpcAccessLogID,
							"grpcService": map[string]interface{}{
								"envoyGrpc": map[string]interface{}{
									"clusterName": "log-cluster",
								},
							},
						},
						"additionalRequestHeadersToLog": log.EnvoyGRPCRequestHeaders(
							log.TbnAccessFormat,
						),
					}),
				},
			},
		},
	)
}

func TestEmptyRequestEmptyDomains(t *testing.T) {
	objects := poller.MkFixtureObjects()
	objects.Domains = nil
	s := lds{}

	resources, err := s.resourceAdapter(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.Equal(t, len(resources.Items), 0)
}

func TestEmptyRequestSeveralDomains(t *testing.T) {
	objects := poller.MkFixtureObjects()
	s := lds{}

	resources, err := s.resourceAdapter(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.NonNil(t, resources.Items)

	httpConnMgr8080, err := s.mkHTTPConnectionManager("main-test-proxy", 8080)
	assert.Nil(t, err)

	httpFilter8080, err := messageToStruct(httpConnMgr8080)
	assert.Nil(t, err)

	httpConnMgr8443, err := s.mkHTTPConnectionManager("main-test-proxy", 8443)
	assert.Nil(t, err)

	httpFilter8443, err := messageToStruct(httpConnMgr8443)
	assert.Nil(t, err)

	expectedEnvoyListeners := []envoyapi.Listener{
		{
			Name: "main-test-proxy:8080",
			Address: envoycore.Address{
				Address: &envoycore.Address_SocketAddress{
					SocketAddress: &envoycore.SocketAddress{
						Protocol: envoycore.TCP,
						Address:  "0.0.0.0",
						PortSpecifier: &envoycore.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
			FilterChains: []envoylistener.FilterChain{
				{
					FilterChainMatch: &envoylistener.FilterChainMatch{},
					Filters: []envoylistener.Filter{
						{
							Name:   util.HTTPConnectionManager,
							Config: httpFilter8080,
						},
					},
				},
			},
		},
		{
			Name: "main-test-proxy:8443",
			Address: envoycore.Address{
				Address: &envoycore.Address_SocketAddress{
					SocketAddress: &envoycore.SocketAddress{
						Protocol: envoycore.TCP,
						Address:  "0.0.0.0",
						PortSpecifier: &envoycore.SocketAddress_PortValue{
							PortValue: 8443,
						},
					},
				},
			},
			FilterChains: []envoylistener.FilterChain{
				{
					FilterChainMatch: &envoylistener.FilterChainMatch{
						SniDomains: []string{"foo.example.com"},
					},
					TlsContext: &envoyauth.DownstreamTlsContext{
						CommonTlsContext: &envoyauth.CommonTlsContext{
							TlsCertificates: []*envoyauth.TlsCertificate{
								{
									CertificateChain: mkFileDataSource("/rotor/xds/poller/example-com.crt"),
									PrivateKey:       mkFileDataSource("/rotor/xds/poller/example-com.key"),
								},
							},
						},
					},
					Filters: []envoylistener.Filter{
						{
							Name:   util.HTTPConnectionManager,
							Config: httpFilter8443,
						},
					},
				},
			},
		},
	}
	checkListeners(t, resources, expectedEnvoyListeners)
}

func TestFileDataSource(t *testing.T) {
	want := &envoycore.DataSource{
		Specifier: &envoycore.DataSource_Filename{
			Filename: "/foo/bar",
		},
	}
	assert.DeepEqual(t, mkFileDataSource("/foo/bar"), want)
}
