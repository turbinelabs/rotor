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
	"sort"
	"testing"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
)

const rdsTestDefaultTimeout = 15 * time.Second

type routeConfigsByName []envoyapi.RouteConfiguration

var _ sort.Interface = routeConfigsByName{}

func (b routeConfigsByName) Len() int           { return len(b) }
func (b routeConfigsByName) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b routeConfigsByName) Less(i, j int) bool { return b[i].Name < b[j].Name }

func checkRouteConfigurations(
	t *testing.T,
	actual cache.Resources,
	expected []envoyapi.RouteConfiguration,
) {
	assert.Equal(t, len(actual.Items), len(expected))

	var actualConfigs []envoyapi.RouteConfiguration
	for _, resource := range actual.Items {
		if route, ok := resource.(*envoyapi.RouteConfiguration); !ok {
			assert.Failed(t, "could not cast resource to RouteConfiguration")
		} else {
			actualConfigs = append(actualConfigs, *route)
		}
	}

	sort.Sort(sort.Reverse(routeConfigsByName(actualConfigs)))

	assert.ArrayEqual(t, actualConfigs, expected)
}

func TestEmptyRoutes(t *testing.T) {
	objects := poller.MkFixtureObjects()
	objects.Domains = nil

	s := rds{rdsTestDefaultTimeout}

	resources, err := s.adapt(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.Equal(t, len(resources.Items), 0)
}

func TestAllPortsRequest(t *testing.T) {
	objects := poller.MkFixtureObjects()

	objects.Domains = append(objects.Domains, tbnapi.Domain{
		DomainKey: tbnapi.DomainKey("D2"),
		ZoneKey:   tbnapi.ZoneKey("Z0"),
		Name:      "baz.example.com",
		Port:      8080,
		Redirects: tbnapi.Redirects{
			{
				Name:         "force-https",
				From:         "(.*)",
				To:           "https://$host$1",
				RedirectType: tbnapi.PermanentRedirect,
			},
			{
				Name:         "go_away",
				From:         "(.*)",
				To:           "https://$host",
				RedirectType: tbnapi.PermanentRedirect,
				HeaderConstraints: tbnapi.HeaderConstraints{{
					Name:   "X-Crazy-Header",
					Value:  "crazy",
					Invert: true,
				}},
			},
		},
	})

	objects.Routes = append(objects.Routes, tbnapi.Route{
		RouteKey:       tbnapi.RouteKey("R3"),
		DomainKey:      tbnapi.DomainKey("D2"),
		ZoneKey:        tbnapi.ZoneKey("Z0"),
		SharedRulesKey: tbnapi.SharedRulesKey("SRK-0"),
		Path:           "/",
		RetryPolicy: &tbnapi.RetryPolicy{
			NumRetries:        5,
			PerTryTimeoutMsec: 500,
			TimeoutMsec:       1000,
		},
	})

	// should get skipped, so it doesn't matter that it isn't fully formed
	objects.Routes[0].Rules = append(objects.Routes[0].Rules, tbnapi.Rule{
		Matches: []tbnapi.Match{
			{
				Kind: tbnapi.QueryMatchKind,
			},
		},
	})

	objects.SharedRules[0].RetryPolicy = &tbnapi.RetryPolicy{
		NumRetries:        12,
		PerTryTimeoutMsec: 34,
	}

	objects.Proxy.DomainKeys = append(objects.Proxy.DomainKeys, tbnapi.DomainKey("D2"))

	objects.Domains[0].Redirects = append(
		objects.Domains[0].Redirects,
		tbnapi.Redirect{
			Name:         "tbnctl-guide",
			From:         "/docs/versions/1.0/guides/tbnctl-guide",
			To:           "https://$host/guides/tbnctlguide.html#somewhere",
			RedirectType: tbnapi.TemporaryRedirect,
		},
		tbnapi.Redirect{
			Name: "api-explorer",
			From: "doesn't matter",
			To:   "//*2317ybkjafs-127123not at all a URL!",
		},
		tbnapi.Redirect{
			Name:         "super broken!",
			From:         "/api-explorer(.*)",
			To:           "https://explorer.turbinelabs.io/api-explorer$1",
			RedirectType: tbnapi.TemporaryRedirect,
		},
		tbnapi.Redirect{
			Name:         "force-www",
			From:         "(.*)",
			To:           "https://www.turbinelabs.io$1",
			RedirectType: tbnapi.PermanentRedirect,
			HeaderConstraints: []tbnapi.HeaderConstraint{
				{
					Name:          "Host",
					Value:         "^turbinelabs.io$",
					CaseSensitive: false,
					Invert:        true,
				},
			},
		},
		tbnapi.Redirect{
			Name:         "docs-versions-v1",
			From:         "/docs/versions/1.0(.*)",
			To:           "https://$host/",
			RedirectType: tbnapi.TemporaryRedirect,
		},
	)

	s := rds{rdsTestDefaultTimeout}

	resources, err := s.adapt(objects)

	assert.Equal(t, resources.Version, objects.TerribleHash())
	assert.Nil(t, err)
	assert.NonNil(t, resources.Items)

	expected := []envoyapi.RouteConfiguration{
		{
			Name: "main-test-proxy:8443",
			VirtualHosts: []envoyroute.VirtualHost{
				{
					Name:       "foo.example.com-8443",
					Domains:    []string{"foo.example.com", "*.example.io", "localhost"},
					RequireTls: envoyroute.VirtualHost_ALL,
					RequestHeadersToAdd: []*envoycore.HeaderValueOption{
						{
							Header: &envoycore.HeaderValue{
								Key:   headerDomainKey,
								Value: "foo.example.com:8443",
							},
							Append: boolValue(false),
						},
					},
					Routes: []envoyroute.Route{
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: ".*/redirect",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "duckduckgo.com",
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: "/docs/versions/1.0/guides/tbnctl-guide",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "foo.example.com",
									PathRewriteSpecifier: &envoyroute.RedirectAction_PathRedirect{
										PathRedirect: "/guides/tbnctlguide.html#somewhere",
									},
									ResponseCode: envoyroute.RedirectAction_TEMPORARY_REDIRECT,
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: "/api-explorer(.*)",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "explorer.turbinelabs.io",
									PathRewriteSpecifier: &envoyroute.RedirectAction_PathRedirect{
										PathRedirect: "/api-explorer",
									},
									ResponseCode: envoyroute.RedirectAction_TEMPORARY_REDIRECT,
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: "(.*)",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: "Host",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^((?!^turbinelabs\\.io$).)*$",
										},
									},
								},
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "www.turbinelabs.io",
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: "/docs/versions/1.0(.*)",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "foo.example.com",
									PathRewriteSpecifier: &envoyroute.RedirectAction_PathRedirect{
										PathRedirect: "/",
									},
									ResponseCode: envoyroute.RedirectAction_TEMPORARY_REDIRECT,
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foo/bar",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: "Cookie",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^(.+?;\\s{0,1})?(TbnBuild=cookie_monster)(;\\s{0,1}(.*?))?$",
										},
									},
									{
										Name: "X-BetaSwVersion",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "93bf93b",
										},
									},
									{
										Name: "extra",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "flavor",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"build":      valueString("cookie_monster"),
																	"stage":      valueString("prod"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(100),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "R1R0",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foo/bar",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: "Cookie",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^(.+?;\\s{0,1})?(TbnBuild=kermit)(;\\s{0,1}(.*?))?$",
										},
									},
									{
										Name: "X-BetaSwVersion",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "93bf93b",
										},
									},
									{
										Name: "extra",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "flavor",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(1),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"build":      valueString("kermit"),
																	"stage":      valueString("canary"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC1",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(1),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "R1R0",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foo/bar",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: ":method",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^(GET|POST|HEAD)$",
										},
									},
									{
										Name: "X-AlphaSwVersion",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "93bf93b",
										},
									},
									{
										Name: "extra",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "flavor",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage":      valueString("canary"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(100),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "R1R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foo/bar",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: ":method",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^(GET|POST|HEAD)$",
										},
									},
									{
										Name: "X-AlphaSwVersion",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "alpha",
										},
									},
									{
										Name: "X-Build",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "blue",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"build":      valueString("cookie_monster"),
																	"stage":      valueString("prod"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(100),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "R1R2",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foo/bar",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("prod"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
												{
													Name:   "foo",
													Weight: uint32Value(1),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("canary"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC1",
															},
															Append: boolValue(false),
														},
													},
												},
												{
													Name:          "baz",
													Weight:        uint32Value(1),
													MetadataMatch: &envoycore.Metadata{},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC2",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(102),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R1",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "SRK-1",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(1),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("prod"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(1),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(34 * time.Millisecond),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(12),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R0",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "SRK-0",
									},
									Append: boolValue(false),
								},
							},
						},
					},
					Cors: &envoyroute.CorsPolicy{
						AllowOrigin:      []string{"*"},
						AllowMethods:     "GET,POST,PUT,DELETE",
						AllowHeaders:     "X-Turbine-Api-Key,Authorization",
						ExposeHeaders:    "foo,bar-baz",
						MaxAge:           "60",
						AllowCredentials: boolValue(true),
						Enabled:          boolValue(true),
					},
				},
			},
		},
		{
			Name: "main-test-proxy:8080",
			VirtualHosts: []envoyroute.VirtualHost{
				{
					Name:    "bar.example.com-8080",
					Domains: []string{"bar.example.com"},
					RequestHeadersToAdd: []*envoycore.HeaderValueOption{
						{
							Header: &envoycore.HeaderValue{
								Key:   headerDomainKey,
								Value: "bar.example.com:8080",
							},
							Append: boolValue(false),
						},
					},
					Routes: []envoyroute.Route{
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foos/bars",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: "X-Requested-With",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "XMLHttpRequest",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "bar",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"flavor": valueString("xml"),
																	"stage":  valueString("dev"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
													ResponseHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   "x-test-header",
																Value: "value-from-route",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "X-Header-Nested",
																Value: "header-value",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "cookie-name=cookie-value; Expires=Mon, 1 Jan 0001 12:00:00 UTC; Secure; HttpOnly",
															},
															Append: boolValue(true),
														},
													},
												},
											},
											TotalWeight: uint32Value(100),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R2",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "R2R0",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foos/bars",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: ":method",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "DELETE",
										},
									},
									{
										Name: "Cookie",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^(.+?;\\s{0,1})?(TbnBuild=kermit)(;\\s{0,1}(.*?))?$",
										},
									},
									{
										Name: "X-BetaSwVersion",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
											ExactMatch: "93bf93b",
										},
									},
								},
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(67),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"build":      valueString("kermit"),
																	"stage":      valueString("canary"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
													ResponseHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   "x-test-header",
																Value: "value-from-route",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "cookie-name=value-from-sr",
															},
															Append: boolValue(true),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "stage=%UPSTREAM_METADATA([\"envoy.lb\",\"stage\"])%; Max-Age=600",
															},
															Append: boolValue(true),
														},
													},
												},
												{
													Name:   "bar",
													Weight: uint32Value(16),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"build":      valueString("kermit"),
																	"stage":      valueString("prod"),
																	"sw_version": valueString("93bf93b"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC1",
															},
															Append: boolValue(false),
														},
													},
													ResponseHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   "x-test-header",
																Value: "value-from-route",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "cookie-name=value-from-sr",
															},
															Append: boolValue(true),
														},
													},
												},
											},
											TotalWeight: uint32Value(83),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R2",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "SRK2-R2R0",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "SRK-2",
									},
									Append: boolValue(false),
								},
							}},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/foos/bars",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "bar",
													Weight: uint32Value(100),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("prod"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
													ResponseHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   "x-test-header",
																Value: "value-from-route",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "cookie-name=value-from-sr",
															},
															Append: boolValue(true),
														},
													},
												},
												{
													Name:   "bar",
													Weight: uint32Value(1),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("canary"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC1",
															},
															Append: boolValue(false),
														},
													},
													ResponseHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   "x-test-header",
																Value: "value-from-route",
															},
															Append: boolValue(false),
														},
														{
															Header: &envoycore.HeaderValue{
																Key:   "Set-Cookie",
																Value: "cookie-name=value-from-sr",
															},
															Append: boolValue(true),
														},
													},
												},
											},
											TotalWeight: uint32Value(101),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(rdsTestDefaultTimeout),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(rdsDefaultNumRetries),
									},
									Timeout: ptr.Duration(rdsTestDefaultTimeout),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R2",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "SRK-2",
									},
									Append: boolValue(false),
								},
							},
						},
					},
				},
				{
					Name:       "baz.example.com-8080",
					Domains:    []string{"baz.example.com"},
					RequireTls: envoyroute.VirtualHost_EXTERNAL_ONLY,
					RequestHeadersToAdd: []*envoycore.HeaderValueOption{
						{
							Header: &envoycore.HeaderValue{
								Key:   headerDomainKey,
								Value: "baz.example.com:8080",
							},
							Append: boolValue(false),
						},
					},
					Routes: []envoyroute.Route{
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Regex{
									Regex: "(.*)",
								},
								CaseSensitive: boolValue(false),
								Headers: []*envoyroute.HeaderMatcher{
									{
										Name: "X-Crazy-Header",
										HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
											RegexMatch: "^((?!crazy).)*$",
										},
									},
								},
							},
							Action: &envoyroute.Route_Redirect{
								Redirect: &envoyroute.RedirectAction{
									HostRedirect: "baz.example.com",
								},
							},
						},
						{
							Match: envoyroute.RouteMatch{
								PathSpecifier: &envoyroute.RouteMatch_Prefix{
									Prefix: "/",
								},
								CaseSensitive: boolValue(false),
							},
							Action: &envoyroute.Route_Route{
								Route: &envoyroute.RouteAction{
									Priority: envoycore.RoutingPriority_DEFAULT,
									ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
										WeightedClusters: &envoyroute.WeightedCluster{
											Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
												{
													Name:   "foo",
													Weight: uint32Value(1),
													MetadataMatch: &envoycore.Metadata{
														FilterMetadata: map[string]*types.Struct{
															"envoy.lb": {
																Fields: map[string]*types.Value{
																	"stage": valueString("prod"),
																},
															},
														},
													},
													RequestHeadersToAdd: []*envoycore.HeaderValueOption{
														{
															Header: &envoycore.HeaderValue{
																Key:   headerConstraintKey,
																Value: "CC0",
															},
															Append: boolValue(false),
														},
													},
												},
											},
											TotalWeight: uint32Value(1),
										},
									},
									RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
										PerTryTimeout: ptr.Duration(500 * time.Millisecond),
										RetryOn:       rdsRetryOn,
										NumRetries:    uint32Value(5),
									},
									Timeout: ptr.Duration(1 * time.Second),
								},
							},
							RequestHeadersToAdd: []*envoycore.HeaderValueOption{
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRouteKey,
										Value: "R3",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerRuleKey,
										Value: "DEFAULT",
									},
									Append: boolValue(false),
								},
								{
									Header: &envoycore.HeaderValue{
										Key:   headerSharedRulesKey,
										Value: "SRK-0",
									},
									Append: boolValue(false),
								},
							},
						},
					},
				},
			},
		},
	}
	checkRouteConfigurations(t, resources, expected)
}

func TestEnvoyHeaderMatcherConversion(t *testing.T) {
	methods := []string{"GET", "POST"}
	input := []requestMatch{
		{
			metadatum: tbnapi.Metadatum{Key: "Key", Value: "Value"},
			matchKind: tbnapi.HeaderMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key2", Value: ""},
			matchKind: tbnapi.HeaderMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key3", Value: "special;chars*"},
			matchKind: tbnapi.HeaderMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key", Value: "Value"},
			matchKind: tbnapi.CookieMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key", Value: ""},
			matchKind: tbnapi.CookieMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "+ooga\\", Value: "booga"},
			matchKind: tbnapi.CookieMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "!#$%&'()*+-./:<=>?@^_`{|}~,=", Value: "wow"},
			matchKind: tbnapi.CookieMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key", Value: "Value"},
			matchKind: tbnapi.QueryMatchKind,
		},
	}

	expected := []*envoyroute.HeaderMatcher{
		{
			Name: ":method",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: "^(GET|POST|HEAD)$",
			},
		},
		{
			Name: "Cookie",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: "^(.+?;\\s{0,1})?(!#\\$%&'\\(\\)\\*\\+-\\./:<=>\\?@\\^_`\\{\\|\\}~,==wow)(;\\s{0,1}(.*?))?$",
			},
		},
		{
			Name: "Cookie",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: "^(.+?;\\s{0,1})?(Key=(.+?))(;\\s{0,1}(.+?))?$",
			},
		},
		{
			Name: "Cookie",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: "^(.+?;\\s{0,1})?(Key=Value)(;\\s{0,1}(.*?))?$",
			},
		},
		{
			Name: "Cookie",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: "^(.+?;\\s{0,1})?(\\+ooga\\\\=booga)(;\\s{0,1}(.*?))?$",
			},
		},
		{
			Name: "Key",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
				ExactMatch: "Value",
			},
		},
		{
			Name: "Key2",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
				ExactMatch: "",
			},
		},
		{
			Name: "Key3",
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: `special(%3b|;)chars(%2a|\*)`,
			},
		},
	}

	got := mkEnvoyHeaderMatchers(input, methods)
	assert.ArrayEqual(t, got, expected)

	// Test that HEAD isn't always added.
	assert.ArrayEqual(
		t,
		mkEnvoyHeaderMatchers(nil, []string{"PUT", "POST"}),
		[]*envoyroute.HeaderMatcher{
			{
				Name: ":method",
				HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
					RegexMatch: "^(PUT|POST)$",
				},
			},
		},
	)
}

func TestEnvoyQueryParamMatcherConversion(t *testing.T) {
	input := []requestMatch{
		{
			metadatum: tbnapi.Metadatum{Key: "Key", Value: "Value"},
			matchKind: tbnapi.QueryMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key2", Value: ""},
			matchKind: tbnapi.QueryMatchKind,
		},
		{
			metadatum: tbnapi.Metadatum{Key: "Key3", Value: "special;chars*"},
			matchKind: tbnapi.QueryMatchKind,
		},
	}

	expected := []*envoyroute.QueryParameterMatcher{
		{
			Name:  "Key",
			Value: "Value",
			Regex: boolValue(false),
		},
		{
			Name:  "Key2",
			Value: "",
			Regex: boolValue(false),
		},
		{
			Name:  "Key3",
			Value: `special%3bchars%2a`,
			Regex: boolValue(false),
		},
	}

	got := mkEnvoyQueryParamMatchers(input)
	assert.ArrayEqual(t, got, expected)
}

func TestEnvoyResponseHeaderConversion(t *testing.T) {
	input := tbnapi.ResponseData{
		Headers: []tbnapi.HeaderDatum{
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "x-header1",
					Value:          "header-value",
					ValueIsLiteral: true,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "x-header2",
					Value:          `header-"value"-2`,
					ValueIsLiteral: false,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "x-header3",
					Value:          "stripped:\n \U0001F600ok",
					ValueIsLiteral: true,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "x-header4",
					Value:          "header-value-with-%",
					ValueIsLiteral: true,
				},
			},
		},
		Cookies: []tbnapi.CookieDatum{
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "cookie1",
					Value:          "cookie-value",
					ValueIsLiteral: true,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "cookie2",
					Value:          "cookie-value",
					ValueIsLiteral: false,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "cookie3",
					Value:          "escaped:\n \U0001F600ok",
					ValueIsLiteral: true,
				},
			},
			{
				ResponseDatum: tbnapi.ResponseDatum{
					Name:           "cookie4",
					Value:          "with-expiry",
					ValueIsLiteral: true,
				},
				ExpiresInSec: ptr.Uint(100),
			},
		},
	}

	expected := []*envoycore.HeaderValueOption{
		{
			Header: &envoycore.HeaderValue{
				Key:   "x-header1",
				Value: "header-value",
			},
			Append: boolValue(false),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "x-header2",
				Value: `%UPSTREAM_METADATA(["envoy.lb","header-\"value\"-2"])%`,
			},
			Append: boolValue(false),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "x-header3",
				Value: "stripped:%%0a%%20%%f0%%9f%%98%%80ok",
			},
			Append: boolValue(false),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "x-header4",
				Value: "header-value-with-%%25",
			},
			Append: boolValue(false),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "Set-Cookie",
				Value: "cookie1=cookie-value",
			},
			Append: boolValue(true),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "Set-Cookie",
				Value: `cookie2=%UPSTREAM_METADATA(["envoy.lb","cookie-value"])%`,
			},
			Append: boolValue(true),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "Set-Cookie",
				Value: "cookie3=escaped:%%0a%%20%%f0%%9f%%98%%80ok",
			},
			Append: boolValue(true),
		},
		{
			Header: &envoycore.HeaderValue{
				Key:   "Set-Cookie",
				Value: "cookie4=with-expiry; Max-Age=100",
			},
			Append: boolValue(true),
		},
	}

	got, err := mkEnvoyResponseHeaders(input)
	assert.Nil(t, err)
	assert.ArrayEqual(t, got, expected)
}
