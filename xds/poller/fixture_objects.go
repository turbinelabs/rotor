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

package poller

import (
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
)

// FixtureHash is the expected result calling TerribleHash() on the *Objects
// produced by MkFixtureObjects()
const FixtureHash = "Q1CjdgpPn0o+vJDxB7iBUg=="

// MkFixtureObjects produces a *Objects that exercises most of the surface area
// of the API, suitable for testing.
func MkFixtureObjects() *Objects {
	return &Objects{
		Zone: api.Zone{
			ZoneKey: "Z0",
			Name:    "the-zone",
		},
		Proxy: api.Proxy{
			ProxyKey:   "P0",
			ZoneKey:    "Z0",
			Name:       "main-test-proxy",
			DomainKeys: []api.DomainKey{"D0", "D1"},
		},
		Clusters: []api.Cluster{
			{
				ClusterKey: "C0",
				ZoneKey:    "Z0",
				Name:       "foo",
				RequireTLS: true,
				Instances: []api.Instance{
					{
						Host: "1.2.3.4",
						Port: 1234,
						Metadata: api.Metadata{
							{Key: "stage", Value: "prod"},
							{Key: "build", Value: "cookie_monster"},
							{Key: "sw_version", Value: "93bf93b"},
						},
					},
					{
						Host: "1.2.3.5",
						Port: 1235,
						Metadata: api.Metadata{
							{Key: "stage", Value: "canary"},
							{Key: "build", Value: "kermit"},
							{Key: "sw_version", Value: "93bf93b"},
						},
					},
					{
						Host: "1.2.3.6",
						Port: 1236,
						Metadata: api.Metadata{
							{Key: "stage", Value: "dev"},
							{Key: "build", Value: "cookie_monster"},
							{Key: "sw_version", Value: "9d323ec"},
						},
					},
				},
				CircuitBreakers: &api.CircuitBreakers{MaxRetries: ptr.Int(10)},
				OutlierDetection: &api.OutlierDetection{
					Consecutive5xx:                     ptr.Int(100),
					IntervalMsec:                       ptr.Int(100),
					EnforcingConsecutive5xx:            ptr.Int(100),
					EnforcingConsecutiveGatewayFailure: ptr.Int(0),
					EnforcingSuccessRate:               ptr.Int(0),
				},
			},
			{
				ClusterKey: "C3",
				ZoneKey:    "Z0",
				Name:       "baz",
				Instances:  []api.Instance{{Host: "1.2.4.8", Port: 8800}},
			},
			{
				ClusterKey: "C1",
				ZoneKey:    "Z0",
				Name:       "bar",
				Instances: []api.Instance{
					{
						Host:     "1.2.3.7",
						Port:     1237,
						Metadata: api.Metadata{{Key: "stage", Value: "prod"}},
					},
					{
						Host: "1.2.3.8",
						Port: 1238,
						Metadata: api.Metadata{
							{Key: "stage", Value: "prod"},
							{Key: "build", Value: "kermit"},
							{Key: "sw_version", Value: "93bf93b"},
						},
					},
					{
						Host: "1.2.3.9",
						Port: 1239,
						Metadata: api.Metadata{
							{Key: "stage", Value: "dev"},
							{Key: "build", Value: "cookie_monster"},
							{Key: "sw_version", Value: "9d323ec"},
						},
					},
				},
				CircuitBreakers: &api.CircuitBreakers{
					MaxConnections: ptr.Int(2048),
					MaxRequests:    ptr.Int(2048),
				},
				OutlierDetection: &api.OutlierDetection{
					IntervalMsec:                       ptr.Int(30000),
					EnforcingConsecutive5xx:            ptr.Int(0),
					EnforcingConsecutiveGatewayFailure: ptr.Int(0),
					EnforcingSuccessRate:               ptr.Int(100),
				},
			},
		},
		Domains: api.Domains{
			api.Domain{
				DomainKey: "D0",
				ZoneKey:   "Z0",
				Name:      "foo.example.com",
				Port:      8443,
				SSLConfig: &api.SSLConfig{
					CipherFilter: "HIGH:" + api.DefaultCipherFilter,
					Protocols:    nil,
					CertKeyPairs: []api.CertKeyPathPair{{
						// these come from http://fm4dd.com/openssl/certexamples.htm
						CertificatePath: "/rotor/xds/poller/example-com.crt",
						KeyPath:         "/rotor/xds/poller/example-com.key",
					}},
				},
				Redirects: api.Redirects{
					{
						Name:         "force-https",
						From:         "(.*)",
						To:           "https://foo.example.com:8443$1",
						RedirectType: api.PermanentRedirect,
						HeaderConstraints: api.HeaderConstraints{{
							Name:   "X-Forwarded-Proto",
							Value:  "https",
							Invert: true,
						}},
					},
					{
						Name:         "second-redirect",
						From:         ".*/redirect",
						To:           "https://duckduckgo.com",
						RedirectType: api.PermanentRedirect,
					},
				},
				CorsConfig: &api.CorsConfig{
					AllowedOrigins:   []string{"*"},
					AllowCredentials: true,
					ExposedHeaders:   []string{"foo", "bar-baz"},
					MaxAge:           60,
					AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
					AllowedHeaders:   []string{"X-Turbine-Api-Key", "Authorization"},
				},
				Aliases: api.DomainAliases{"*.example.io", "localhost"},
			},
			api.Domain{
				DomainKey:   "D1",
				ZoneKey:     "Z0",
				Name:        "bar.example.com",
				Port:        8080,
				GzipEnabled: true,
				Checksum:    api.Checksum{Checksum: "ajsdfhdskjfh"},
			},
		},
		Routes: []api.Route{
			{
				RouteKey:       "R0",
				DomainKey:      "D0",
				ZoneKey:        "Z0",
				Path:           "/",
				SharedRulesKey: "SRK-0",
			},
			{
				RouteKey:       "R1",
				DomainKey:      "D0",
				ZoneKey:        "Z0",
				Path:           "/foo/bar",
				SharedRulesKey: "SRK-1",
				Rules: []api.Rule{
					{
						RuleKey: "R1R0",
						Matches: []api.Match{
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-BetaSwVersion", Value: ""},
								To:   api.Metadatum{Key: "sw_version", Value: ""},
							},
							{
								Kind: api.CookieMatchKind,
								From: api.Metadatum{Key: "TbnBuild", Value: ""},
								To:   api.Metadatum{Key: "build", Value: ""},
							},
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "extra", Value: "flavor"},
							},
						},
						Constraints: api.AllConstraints{
							Light: api.ClusterConstraints{
								{
									ConstraintKey: "CC0",
									ClusterKey:    "C0",
									Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									Weight:        100,
								},
								{
									ConstraintKey: "CC1",
									ClusterKey:    "C0",
									Metadata:      api.Metadata{{Key: "stage", Value: "canary"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									Weight:        1,
								},
							},
						},
						CohortSeed: &api.CohortSeed{
							Type: "header",
							Name: "x-cohort-route-rule",
						},
					},
					{
						RuleKey: "R1R1",
						Methods: []string{"GET", "POST"},
						Matches: []api.Match{
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-AlphaSwVersion"},
								To:   api.Metadatum{Key: "sw_version"},
							},
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "extra", Value: "flavor"},
							},
						},
						Constraints: api.AllConstraints{
							Light: api.ClusterConstraints{
								{
									ConstraintKey: "CC0",
									ClusterKey:    "C0",
									Metadata:      api.Metadata{{Key: "stage", Value: "canary"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									Weight:        100,
								},
							},
						},
					},
					{
						RuleKey: "R1R2",
						Methods: []string{"GET", "POST"},
						Matches: []api.Match{
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-AlphaSwVersion", Value: "alpha"},
								To:   api.Metadatum{Key: "sw_version", Value: "93bf93b"},
							},
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-Build", Value: "blue"},
								To:   api.Metadatum{Key: "build", Value: "cookie_monster"},
							},
						},
						Constraints: api.AllConstraints{
							Light: api.ClusterConstraints{
								{
									ConstraintKey: "CC0",
									ClusterKey:    "C0",
									Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									Weight:        100,
								},
							},
						},
					},
				},
				CohortSeed: &api.CohortSeed{
					Type:             "query",
					Name:             "x-cohort-route",
					UseZeroValueSeed: true,
				},
			},
			{
				RouteKey:       "R2",
				DomainKey:      "D1",
				ZoneKey:        "Z0",
				Path:           "/foos/bars",
				SharedRulesKey: "SRK-2",
				Rules: []api.Rule{
					{
						RuleKey: "R2R0",
						Matches: []api.Match{
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-Requested-With", Value: "XMLHttpRequest"},
							},
						},
						Constraints: api.AllConstraints{
							Light: api.ClusterConstraints{
								{
									ConstraintKey: "CC0",
									ClusterKey:    "C1",
									Metadata: api.Metadata{
										{Key: "stage", Value: "dev"},
										{Key: "flavor", Value: "xml"},
									},
									Properties: api.Metadata{{Key: "state", Value: "test"}},
									ResponseData: api.ResponseData{
										Headers: []api.HeaderDatum{
											{
												ResponseDatum: api.ResponseDatum{
													Name:           "X-Header-Nested",
													Value:          "header-value",
													ValueIsLiteral: true,
													AlwaysSend:     true,
												},
											},
										},
										Cookies: []api.CookieDatum{
											{
												ResponseDatum: api.ResponseDatum{
													Name:           "cookie-name",
													Value:          "cookie-value",
													ValueIsLiteral: true,
												},
												HttpOnly:     true,
												Secure:       true,
												ExpiresInSec: ptr.Uint(0),
											},
										},
									},
									Weight: 100,
								},
							},
						},
					},
				},
				ResponseData: api.ResponseData{
					Headers: []api.HeaderDatum{
						{
							ResponseDatum: api.ResponseDatum{
								Name:           "x-test-header",
								Value:          "value-from-route",
								ValueIsLiteral: true,
							},
						},
					},
				},
				CohortSeed: &api.CohortSeed{
					Type: "header",
					Name: "x-cohort-shared-rules",
				},
			},
		},
		SharedRules: []api.SharedRules{
			{
				SharedRulesKey: "SRK-0",
				Name:           "SRK-0",
				ZoneKey:        "Z0",
				Default: api.AllConstraints{
					Light: []api.ClusterConstraint{
						{
							ConstraintKey: "CC0",
							ClusterKey:    "C0",
							Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
							Properties:    api.Metadata{{Key: "state", Value: "released"}},
							Weight:        1,
						},
					},
				},
				Properties: api.Metadata{{Key: "srk0pk", Value: "srk0pv"}},
			},
			{
				SharedRulesKey: "SRK-1",
				Name:           "SRK-1",
				ZoneKey:        "Z0",
				Default: api.AllConstraints{
					Light: []api.ClusterConstraint{
						{
							ConstraintKey: "CC0",
							ClusterKey:    "C0",
							Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
							Properties:    api.Metadata{{Key: "state", Value: "released"}},
							Weight:        100,
						},
						{
							ConstraintKey: "CC1",
							ClusterKey:    "C0",
							Metadata:      api.Metadata{{Key: "stage", Value: "canary"}},
							Properties:    api.Metadata{{Key: "state", Value: "released"}},
							Weight:        1,
						},
						{
							ConstraintKey: "CC2",
							ClusterKey:    "C3",
							Weight:        1,
						},
					},
				},
				CohortSeed: &api.CohortSeed{
					Type:             "cookie",
					Name:             "cookie-cohort-shared-rules",
					UseZeroValueSeed: true,
				},
				Properties: api.Metadata{{Key: "srk1pk", Value: "srk1pv"}},
			},
			{
				SharedRulesKey: "SRK-2",
				Name:           "SRK-2",
				ZoneKey:        "Z0",
				Default: api.AllConstraints{
					Light: []api.ClusterConstraint{
						{
							ConstraintKey: "CC0",
							ClusterKey:    "C1",
							Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
							Properties:    api.Metadata{{Key: "state", Value: "released"}},
							Weight:        100,
						},
						{
							ConstraintKey: "CC1",
							ClusterKey:    "C1",
							Metadata:      api.Metadata{{Key: "stage", Value: "canary"}},
							Properties:    api.Metadata{{Key: "state", Value: "released"}},
							Weight:        1,
						},
					},
				},
				Rules: api.Rules{
					{
						RuleKey: "SRK2-R2R0",
						Methods: []string{"DELETE"},
						Matches: []api.Match{
							{
								Kind: api.HeaderMatchKind,
								From: api.Metadatum{Key: "X-BetaSwVersion"},
								To:   api.Metadatum{Key: "sw_version"},
							},
							{
								Kind: api.CookieMatchKind,
								From: api.Metadatum{Key: "TbnBuild"},
								To:   api.Metadatum{Key: "build"},
							},
						},
						Constraints: api.AllConstraints{
							Light: []api.ClusterConstraint{
								{
									ConstraintKey: "CC0",
									ClusterKey:    "C0",
									Metadata:      api.Metadata{{Key: "stage", Value: "canary"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									ResponseData: api.ResponseData{
										Cookies: []api.CookieDatum{
											{
												ResponseDatum: api.ResponseDatum{
													Name:  "stage",
													Value: "stage",
												},
												ExpiresInSec: ptr.Uint(600),
											},
										},
									},
									Weight: 67,
								},
								{
									ConstraintKey: "CC1",
									ClusterKey:    "C1",
									Metadata:      api.Metadata{{Key: "stage", Value: "prod"}},
									Properties:    api.Metadata{{Key: "state", Value: "test"}},
									Weight:        16,
								},
							},
						},
					},
				},
				ResponseData: api.ResponseData{
					Headers: []api.HeaderDatum{
						{
							ResponseDatum: api.ResponseDatum{
								Name:           "X-Test-Header",
								Value:          "value-from-sr",
								ValueIsLiteral: true,
								AlwaysSend:     true,
							},
						},
					},
					Cookies: []api.CookieDatum{
						{
							ResponseDatum: api.ResponseDatum{
								Name:           "cookie-name",
								Value:          "value-from-sr",
								ValueIsLiteral: true,
								AlwaysSend:     true,
							},
						},
					},
				},
				Properties: api.Metadata{{Key: "srk3pk", Value: "srk3pv"}},
			},
		},
	}
}
