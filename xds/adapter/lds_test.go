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
	"bytes"
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoylog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	envoyhcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/jsonpb"
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
						"upstream_log": []map[string]interface{}{
							{
								"name": util.HTTPGRPCAccessLog,
								"config": map[string]interface{}{
									"common_config": map[string]interface{}{
										"log_name": grpcUpstreamLogID,
										"grpc_service": map[string]interface{}{
											"envoy_grpc": map[string]interface{}{
												"cluster_name": "log-cluster",
											},
										},
									},
									"additional_request_headers_to_log": log.EnvoyGRPCRequestHeaders(
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
						"common_config": map[string]interface{}{
							"log_name": grpcAccessLogID,
							"grpc_service": map[string]interface{}{
								"envoy_grpc": map[string]interface{}{
									"cluster_name": "log-cluster",
								},
							},
						},
						"additional_request_headers_to_log": log.EnvoyGRPCRequestHeaders(
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

	resources, err := s.adapt(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.Equal(t, len(resources.Items), 0)
}

func TestEmptyRequestSeveralDomains(t *testing.T) {
	objects := poller.MkFixtureObjects()
	s := lds{}

	resources, err := s.adapt(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.NonNil(t, resources.Items)

	httpConnMgr8080, err := s.mkHTTPConnectionManager("main-test-proxy", 8080)
	assert.Nil(t, err)

	httpFilter8080, err := util.MessageToStruct(httpConnMgr8080)
	assert.Nil(t, err)

	httpConnMgr8443, err := s.mkHTTPConnectionManager("main-test-proxy", 8443)
	assert.Nil(t, err)

	httpFilter8443, err := util.MessageToStruct(httpConnMgr8443)
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

func TestInjectNilListener(t *testing.T) {
	assert.Nil(t, lds{}.inject(nil))
}

func TestInjectNilFilterChains(t *testing.T) {
	want := &envoyapi.Listener{}
	target := &envoyapi.Listener{}
	assert.Nil(t, lds{}.inject(target))
	assert.DeepEqual(t, target, want)
}

type injectTestCase struct {
	before string
	after  string
}

func (tc injectTestCase) run(t *testing.T) {
	buf := bytes.NewBufferString(tc.before)
	listener := &envoyapi.Listener{}
	if err := jsonpb.Unmarshal(buf, listener); err != nil {
		t.Fatal(err)
	}

	buf = bytes.NewBufferString(tc.after)
	want := &envoyapi.Listener{}
	if err := jsonpb.Unmarshal(buf, want); err != nil {
		t.Fatal(err)
	}

	assert.Nil(t, lds{loggingCluster: "logging-cluster"}.inject(listener))

	if !assert.DeepEqual(t, listener, want) {
		t.Error("got:", listener)
		t.Error("want:", want)
	}
}

func TestInject(t *testing.T) {
	injectTestCase{
		before: `{
      "name": "localhost:8080",
      "address": {
        "socket_address": {
          "address": "0.0.0.0",
          "port_value": 8080
        }
      },
      "filter_chains": [
        {
          "filters": [
            {
              "name": "some.other_filter",
              "config": {}
            },
            {
              "name": "envoy.http_connection_manager",
              "config": {
                "route_config": {
                  "virtual_hosts": [
                    {
                      "name": "animals:8080",
                      "domains": [
                        "animals"
                      ],
                      "routes": [
                        {
                          "match": {
                            "path": "/cat"
                          },
                          "route": {
                            "cluster": "cat"
                          }
                        },
                        {
                          "route": {
                            "cluster": "dog"
                          }
                        },
                        {
                          "match": {
                            "prefix": "/"
                          },
                          "route": {
                            "cluster": "animals"
                          }
                        },
                        {
                          "match": {
                            "regex": "$/(dog|wolf)^"
                          },
                          "route": {
                            "cluster": "canine"
                          }
                        }
                      ]
                    }
                  ]
                },
                "http_filters": [
                  {
                    "name": "some.other_filter",
                    "config": {}
                  },
                  {
                    "name": "envoy.router",
                    "config": {}
                  }
                ]
              }
            }
          ]
        }
      ]
    }`,
		after: `{
      "name": "localhost:8080",
      "address": {
        "socket_address": {
          "address": "0.0.0.0",
          "port_value": 8080
        }
      },
      "filter_chains": [
        {
          "filters": [
            {
              "name": "some.other_filter",
              "config": {}
            },
            {
              "name": "envoy.http_connection_manager",
              "config": {
                "access_log": [
                  {
                    "config": {
                      "additional_request_headers_to_log": [
                        "X-TBN-DOMAIN",
                        "X-TBN-ROUTE",
                        "X-TBN-RULE",
                        "X-TBN-SHARED-RULES",
                        "X-TBN-CONSTRAINT"
                      ],
                      "common_config": {
                        "grpc_service": {
                          "envoy_grpc": {
                            "cluster_name": "logging-cluster"
                          }
                        },
                        "log_name": "tbn.access"
                      }
                    },
                    "name": "envoy.http_grpc_access_log"
                  }
                ],
                "route_config": {
                  "virtual_hosts": [
                    {
                      "name": "animals:8080",
                      "domains": [
                        "animals"
                      ],
                      "request_headers_to_add": [
                        {
                          "append": false,
                          "header": {
                            "key": "x-tbn-domain",
                            "value": "animals:8080"
                          }
                        }
                      ],
                      "routes": [
                        {
                          "match": {
                            "path": "/cat"
                          },
                          "route": {
                            "cluster": "cat",
                            "request_headers_to_add": [
                              {
                                "append": false,
                                "header": {
                                  "key": "x-tbn-route",
                                  "value": "animals:8080/cat"
                                }
                              }
                            ]
                          }
                        },
                        {
                          "match": {},
                          "route": {
                            "cluster": "dog",
                            "request_headers_to_add": [
                              {
                                "append": false,
                                "header": {
                                  "key": "x-tbn-route",
                                  "value": "animals:8080/DEFAULT"
                                }
                              }
                            ]
                          }
                        },
                        {
                          "match": {
                            "prefix": "/"
                          },
                          "route": {
                            "cluster": "animals",
                            "request_headers_to_add": [
                              {
                                "append": false,
                                "header": {
                                  "key": "x-tbn-route",
                                  "value": "animals:8080/"
                                }
                              }
                            ]
                          }
                        },
                        {
                          "match": {
                            "regex": "$/(dog|wolf)^"
                          },
                          "route": {
                            "cluster": "canine",
                            "request_headers_to_add": [
                              {
                                "append": false,
                                "header": {
                                  "key": "x-tbn-route",
                                  "value": "animals:8080$/(dog|wolf)^"
                                }
                              }
                            ]
                          }
                        }
                      ]
                    }
                  ]
                },
                "http_filters": [
                  {
                    "name": "some.other_filter",
                    "config": {}
                  },
                  {
                    "name": "envoy.router",
                    "config": {
                      "upstream_log": [
                        {
                          "config": {
                            "additional_request_headers_to_log": [
                              "X-TBN-DOMAIN",
                              "X-TBN-ROUTE",
                              "X-TBN-RULE",
                              "X-TBN-SHARED-RULES",
                              "X-TBN-CONSTRAINT"
                            ],
                            "common_config": {
                              "grpc_service": {
                                "envoy_grpc": {
                                  "cluster_name": "logging-cluster"
                                }
                              },
                              "log_name": "tbn.upstream"
                            }
                          },
                          "name": "envoy.http_grpc_access_log"
                        }
                      ]
                    }
                  }
                ]
              }
            }
          ]
        }
      ]
    }`,
	}.run(t)
}

func TestListenerMapStrictOK(t *testing.T) {
	lm := newListenerMap(true)
	assert.Equal(t, len(lm.resourceMap()), 0)
	err := lm.addListener(mkTestListener("listener-1", "host-1", 1))
	assert.Nil(t, err)
	err = lm.addListener(mkTestListener("listener-2", "host-1", 2))
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 2)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
	assert.Equal(t, lm.resourceMap()["listener-2"].(*envoyapi.Listener).Name, "listener-2")
}

func TestListenerMapStrictDuplicateName(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-1", "host-2", 2))
	assert.ErrorContains(t, err, `duplicate listener name: "listener-1"`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictBadPort(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(&envoyapi.Listener{Name: "listener-2"})
	assert.ErrorContains(t, err, `could not lookup port for listener "listener-2": no socket address found`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictCollision(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.ErrorContains(t, err, `cannot add listener "listener-2" because listener "listener-1" already declared on host-1:1`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictAllPortsIPV4Collision(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "0.0.0.0", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.ErrorContains(t, err, `cannot add listener "listener-2" on host-1:1 because listener "listener-1" already declared on 0.0.0.0:1`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictAllPortsIPV6Collision(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "::", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.ErrorContains(t, err, `cannot add listener "listener-2" on host-1:1 because listener "listener-1" already declared on [::]:1`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictAllPortsIPV4ReverseCollision(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-2", "0.0.0.0", 1))
	assert.ErrorContains(t, err, `cannot add listener "listener-2" on 0.0.0.0:1, because one or more listeners exist for port 1: "listener-1" on host-1:1`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapStrictAllPortsIPV6ReverseCollision(t *testing.T) {
	lm := newListenerMap(true)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-2", "::", 1))
	assert.ErrorContains(t, err, `cannot add listener "listener-2" on [::]:1, because one or more listeners exist for port 1: "listener-1" on host-1:1`)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapLooseOK(t *testing.T) {
	lm := newListenerMap(false)
	err := lm.addListener(mkTestListener("listener-1", "host-1", 1))
	assert.Nil(t, err)
	err = lm.addListener(mkTestListener("listener-2", "host-1", 2))
	assert.Nil(t, err)
}

func TestListenerMapLooseDuplicateName(t *testing.T) {
	lm := newListenerMap(false)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-1", "host-2", 2))
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapLooseBadPort(t *testing.T) {
	lm := newListenerMap(false)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(&envoyapi.Listener{Name: "listener-2"})
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapLooseCollision(t *testing.T) {
	lm := newListenerMap(false)
	lm.addListener(mkTestListener("listener-1", "host-1", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-2"].(*envoyapi.Listener).Name, "listener-2")
}

func TestListenerMapLooseAllPortsIPV4Collision(t *testing.T) {
	lm := newListenerMap(false)
	lm.addListener(mkTestListener("listener-1", "0.0.0.0", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestListenerMapLooseAllPortsIPV6Collision(t *testing.T) {
	lm := newListenerMap(false)
	lm.addListener(mkTestListener("listener-1", "::", 1))
	err := lm.addListener(mkTestListener("listener-2", "host-1", 1))
	assert.Nil(t, err)
	assert.Equal(t, len(lm.resourceMap()), 1)
	assert.Equal(t, lm.resourceMap()["listener-1"].(*envoyapi.Listener).Name, "listener-1")
}

func TestHostPortForListenerNilListener(t *testing.T) {
	gotHost, gotPort, gotErr := hostPortForListener(nil)
	assert.Equal(t, gotHost, "")
	assert.Equal(t, gotPort, 0)
	assert.ErrorContains(t, gotErr, "could not lookup port for nil listener")
}

func TestHostPortForListenerNoSocketAddress(t *testing.T) {
	l := &envoyapi.Listener{Name: "the listener"}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "")
	assert.Equal(t, gotPort, 0)
	assert.ErrorContains(t, gotErr, `could not lookup port for listener "the listener": no socket address found`)
}

func TestHostPortForListenerNoPort(t *testing.T) {
	l := &envoyapi.Listener{
		Name: "the listener",
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{},
			},
		},
	}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "")
	assert.Equal(t, gotPort, 0)
	assert.ErrorContains(t, gotErr, `could not lookup port for listener "the listener": has neither a fixed nor named port`)
}

func TestHostPortForListenerPortValue(t *testing.T) {
	l := &envoyapi.Listener{
		Name: "the listener",
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Address: "the address",
					PortSpecifier: &envoycore.SocketAddress_PortValue{
						PortValue: 1234,
					},
				},
			},
		},
	}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "the address")
	assert.Equal(t, gotPort, 1234)
	assert.Nil(t, gotErr)
}

func TestHostPortForListenerBadNamedPort(t *testing.T) {
	l := &envoyapi.Listener{
		Name: "the listener",
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					PortSpecifier: &envoycore.SocketAddress_NamedPort{
						NamedPort: "some garbage",
					},
				},
			},
		},
	}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "")
	assert.Equal(t, gotPort, 0)
	assert.ErrorContains(t, gotErr, `could not lookup port for listener "the listener"`)
}

func TestHostPortForListenerNamedPortNoProto(t *testing.T) {
	l := &envoyapi.Listener{
		Name: "the listener",
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Address: "the address",
					PortSpecifier: &envoycore.SocketAddress_NamedPort{
						NamedPort: "http",
					},
				},
			},
		},
	}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "the address")
	assert.Equal(t, gotPort, 80)
	assert.Nil(t, gotErr)
}

func TestHostPortForListenerNamedPortWithProto(t *testing.T) {
	l := &envoyapi.Listener{
		Name: "the listener",
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Protocol: envoycore.UDP,
					Address:  "the address",
					PortSpecifier: &envoycore.SocketAddress_NamedPort{
						NamedPort: "ssh",
					},
				},
			},
		},
	}
	gotHost, gotPort, gotErr := hostPortForListener(l)
	assert.Equal(t, gotHost, "the address")
	assert.Equal(t, gotPort, 22)
	assert.Nil(t, gotErr)
}

func TestAddHeaderIfMissing(t *testing.T) {
	want := []*envoycore.HeaderValueOption{
		{},
		{
			Header: &envoycore.HeaderValue{
				Key:   "the-key",
				Value: "some-other-value",
			},
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "some-other-key",
				Value: "some-other-value",
			},
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "the-key",
				Value: "the-value",
			},
			Append: boolValue(false),
		},
	}

	// not there
	got := addHeaderIfMissing("the-key", "the-value", want[0:3])
	assert.DeepEqual(t, got, want)

	// already there
	got = addHeaderIfMissing("the-key", "the-value", want)
	assert.DeepEqual(t, got, want)
}
