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
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/envoyproxy/go-control-plane/pkg/cache"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/arrays/indexof"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor/xds/poller"
)

const (
	rdsRetryOn           = "connect-failure,refused-stream,gateway-error"
	rdsDefaultNumRetries = 2

	// Header names must remain all-lowercase.
	headerDomainKey      = "x-tbn-domain"
	headerRouteKey       = "x-tbn-route"
	headerRuleKey        = "x-tbn-rule"
	headerSharedRulesKey = "x-tbn-shared-rules"
	headerConstraintKey  = "x-tbn-constraint"

	cookieHeaderName  = "Cookie"
	envoyMethodHeader = ":method"

	exactCookieMatchTemplate    = "^(.+?;\\s{0,1})?(%s=%s)(;\\s{0,1}(.*?))?$"
	wildcardCookieMatchTemplate = "^(.+?;\\s{0,1})?(%s=(.+?))(;\\s{0,1}(.+?))?$"
	inverseValueMatchTemplate   = "^((?!%s).)*$"
)

var captureGroupRegex = regexp.MustCompile("\\$[0-9]+")

type rds struct {
	defaultTimeout time.Duration
}

type rdsResourceNameAndPort struct {
	ResourceName string
	Port         int
}

type rdsResourceConfig struct {
	ResourceName  string
	DomainConfigs []domainConfig
}

// Grouping of all objects we need for a given domain
type domainConfig struct {
	Domain tbnapi.Domain
	Routes []routeConfig
}

type routeConfig struct {
	Route       tbnapi.Route
	SharedRules tbnapi.SharedRules
	AllRules    []poller.TaggedRule
}

// reverse sort by prefix, so the longest prefix is considered first.
type routeByPrefixReverse []routeConfig

var _ sort.Interface = routeByPrefixReverse{}

func (b routeByPrefixReverse) Len() int      { return len(b) }
func (b routeByPrefixReverse) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b routeByPrefixReverse) Less(i, j int) bool {
	return b[i].Route.Path > b[j].Route.Path
}

// adapt turns poller.Objects into Route cache.Resources
func (s rds) adapt(objects *poller.Objects) (cache.Resources, error) {
	rcs, err := s.getResourceConfigs(objects)
	if err != nil {
		return cache.Resources{}, err
	}

	resources := map[string]cache.Resource{}
	for _, rc := range rcs {
		routeConfig, err := s.mkEnvoyRouteConfig(rc.ResourceName, rc.DomainConfigs, objects)
		if err != nil {
			return cache.Resources{}, err
		}
		resources[routeConfig.GetName()] = routeConfig
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
}

// We expect each resourceName to be "${Proxy.Name}:${Port}". Each resourceName
// maps to many domainConfigs since there can be multiple `tbn.Domain` for a
// given port.  The return structure ensures we return only a single set of
// domainConfigs per proxy/port pair.
func (s rds) getResourceConfigs(
	objects *poller.Objects,
) ([]rdsResourceConfig, error) {
	var resourceNamesAndPorts []rdsResourceNameAndPort

	// force stable ordering
	ports := objects.AllPorts()
	sort.Sort(sort.Reverse(sort.IntSlice(ports)))

	for _, p := range ports {
		resourceName := mkListenerName(objects.Proxy.Name, p)
		rnap := rdsResourceNameAndPort{ResourceName: resourceName, Port: p}
		resourceNamesAndPorts = append(resourceNamesAndPorts, rnap)
	}

	var resourceConfigs []rdsResourceConfig
	for _, rnap := range resourceNamesAndPorts {
		domainConfigs := []domainConfig{}
		for _, domain := range objects.DomainsPerPort(rnap.Port) {
			routeConfigs := []routeConfig{}
			for _, rt := range objects.RoutesPerDomain(domain.DomainKey) {
				sharedRules := objects.SharedRulesFromKey(rt.SharedRulesKey)
				// fill in default retry policy
				s.fixRetryPolicy(&rt, sharedRules)
				routeConfig := routeConfig{
					Route:       rt,
					SharedRules: sharedRules,
					AllRules:    objects.AggregateRules(rt),
				}
				routeConfigs = append(routeConfigs, routeConfig)
			}
			// sort reverse by prefix, so the longest (and therefore most specific)
			// prefix matches first.
			sort.Sort(routeByPrefixReverse(routeConfigs))
			domainConfig := domainConfig{Domain: domain, Routes: routeConfigs}
			domainConfigs = append(domainConfigs, domainConfig)
		}
		resourceConfigs = append(resourceConfigs,
			rdsResourceConfig{
				ResourceName:  rnap.ResourceName,
				DomainConfigs: domainConfigs,
			},
		)
	}

	return resourceConfigs, nil
}

// TODO(https://github.com/turbinelabs/tbn/issues/4558): consider allowing
// specification of default perTryTimeout from the command-line as well.
func (s rds) fixRetryPolicy(rt *tbnapi.Route, srs tbnapi.SharedRules) {
	defaultTimeoutMsec := int(s.defaultTimeout / time.Millisecond)

	// if nil, fill in with RetryPolicy from shared rules
	if rt.RetryPolicy == nil {
		rt.RetryPolicy = srs.RetryPolicy
	}

	// if still nil, fill in with default retry policy
	if rt.RetryPolicy == nil {
		rt.RetryPolicy = &tbnapi.RetryPolicy{
			NumRetries:        rdsDefaultNumRetries,
			PerTryTimeoutMsec: defaultTimeoutMsec,
			TimeoutMsec:       defaultTimeoutMsec,
		}
	}

	// use default if set to zero
	if rt.RetryPolicy.PerTryTimeoutMsec == 0 {
		rt.RetryPolicy.PerTryTimeoutMsec = defaultTimeoutMsec
	}

	// use default if set to zero
	if rt.RetryPolicy.TimeoutMsec == 0 {
		rt.RetryPolicy.TimeoutMsec = defaultTimeoutMsec
	}
}

func (s rds) mkEnvoyRouteConfig(
	name string,
	domainConfigs []domainConfig,
	objects *poller.Objects,
) (*envoyapi.RouteConfiguration, error) {
	virtualHosts := []envoyroute.VirtualHost{}
	for _, domainConfig := range domainConfigs {
		virtualHost, err := mkEnvoyVirtualHost(domainConfig, objects)
		if err != nil {
			return nil, err
		}
		virtualHosts = append(virtualHosts, *virtualHost)
	}

	return &envoyapi.RouteConfiguration{
		Name:         name,
		VirtualHosts: virtualHosts,
	}, nil
}

func mkEnvoyVirtualHost(
	domainConfig domainConfig,
	objects *poller.Objects,
) (*envoyroute.VirtualHost, error) {
	var corsPolicy *envoyroute.CorsPolicy

	tlsReq, cleanDomainConfig := resolveTLSRequirement(domainConfig)
	if cleanDomainConfig.Domain.CorsConfig != nil {
		corsPolicy = mkEnvoyCorsPolicy(cleanDomainConfig.Domain.CorsConfig)
	}

	domains := []string{cleanDomainConfig.Domain.Name, cleanDomainConfig.Domain.Addr()}
	for _, alias := range cleanDomainConfig.Domain.Aliases {
		domains = append(
			domains,
			string(alias),
			fmt.Sprintf("%s:%d", alias, cleanDomainConfig.Domain.Port),
		)
	}

	routes, err := mkEnvoyRoutes(cleanDomainConfig, objects)
	if err != nil {
		return nil, err
	}
	return &envoyroute.VirtualHost{
		Name:       mkStatsName(cleanDomainConfig.Domain.Name, cleanDomainConfig.Domain.Port),
		Domains:    domains,
		RequireTls: tlsReq,
		Routes:     routes,
		Cors:       corsPolicy,
		RequestHeadersToAdd: []*envoycore.HeaderValueOption{
			{
				Header: &envoycore.HeaderValue{
					Key: headerDomainKey,
					Value: valueOrDefault(mkListenerName(
						cleanDomainConfig.Domain.Name,
						cleanDomainConfig.Domain.Port,
					)),
				},
				Append: boolValue(false),
			},
		},
	}, nil
}

func mkEnvoyCorsPolicy(corsConfig *tbnapi.CorsConfig) *envoyroute.CorsPolicy {
	return &envoyroute.CorsPolicy{
		AllowOrigin:      corsConfig.AllowedOrigins,
		AllowMethods:     strings.Join(corsConfig.AllowedMethods, ","),
		AllowHeaders:     strings.Join(corsConfig.AllowedHeaders, ","),
		ExposeHeaders:    strings.Join(corsConfig.ExposedHeaders, ","),
		MaxAge:           fmt.Sprintf("%d", corsConfig.MaxAge),
		AllowCredentials: boolValue(corsConfig.AllowCredentials),
		Enabled:          boolValue(true),
	}
}

// Envoy honors XFP headers when enforcing TLS so we translate https redirects
// by specifying a requirement other than VirtualHost_NONE. If we don't have an SSLConfig
// configured, we only trust "internal requests" as specified by RFC-1918.
func resolveTLSRequirement(
	domainCfg domainConfig,
) (envoyroute.VirtualHost_TlsRequirementType, domainConfig) {
	var tlsReq envoyroute.VirtualHost_TlsRequirementType
	switch {
	case domainCfg.Domain.SSLConfig != nil:
		tlsReq = envoyroute.VirtualHost_ALL
	case domainCfg.Domain.ForceHTTPS:
		tlsReq = envoyroute.VirtualHost_EXTERNAL_ONLY
	default:
		tlsReq = envoyroute.VirtualHost_NONE
	}

	return tlsReq, domainCfg
}

func mkEnvoyRoutes(
	domainConfig domainConfig,
	objects *poller.Objects,
) ([]envoyroute.Route, error) {
	routes := tbnRedirectToEnvoyRoutes(domainConfig.Domain)

	prefixRoutes, err := tbnToEnvoyRoutes(domainConfig, objects)
	if err != nil {
		return nil, err
	}

	routes = append(routes, prefixRoutes...)
	return routes, nil
}

// Since envoy redirect actions don't currently support:
//   - modifying the destination protocol
//   - using capture groups in the destination address(e.g. '$1', '$2', etc)
//   - using nginx style variables(e.g. '$host')
// this method ensures we provide envoy with a valid route by stripping
// capture groups and resolving nginx variables.
func tbnRedirectToEnvoyRoutes(domain tbnapi.Domain) []envoyroute.Route {
	routes := []envoyroute.Route{}
	for _, redirect := range domain.Redirects {
		url, err := url.Parse(redirect.To)
		if err != nil {
			console.Error().Printf(
				"Invalid Redirect destination for Domain[%s:%d], Redirect[%s]",
				domain.Name,
				domain.Port,
				redirect.Name,
			)
			continue
		}

		var responseCode envoyroute.RedirectAction_RedirectResponseCode
		switch redirect.RedirectType {
		case tbnapi.TemporaryRedirect:
			responseCode = envoyroute.RedirectAction_TEMPORARY_REDIRECT
		case tbnapi.PermanentRedirect:
			responseCode = envoyroute.RedirectAction_MOVED_PERMANENTLY
		}

		redirectHost := url.Hostname()
		if redirectHost == "$host" {
			redirectHost = domain.Name
		}

		path := url.EscapedPath()
		if url.Fragment != "" {
			path += "#" + url.Fragment
		}

		redirectAction := &envoyroute.RedirectAction{
			HostRedirect: scrubCaptureGroups(redirectHost),
			ResponseCode: responseCode,
		}
		if url.Scheme == "https" {
			redirectAction.HttpsRedirect = true
		}

		pathRewrite := scrubCaptureGroups(path)
		if pathRewrite != "" {
			redirectAction.PathRewriteSpecifier = &envoyroute.RedirectAction_PathRedirect{
				PathRedirect: pathRewrite,
			}
		}

		route := envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Regex{Regex: redirect.From},
				CaseSensitive: boolValue(false),
				Headers:       toEnvoyHeaderMatcher(domain, redirect),
			},
			Action: &envoyroute.Route_Redirect{
				Redirect: redirectAction,
			},
		}

		routes = append(routes, route)
	}

	return routes
}

func scrubCaptureGroups(str string) string {
	return captureGroupRegex.ReplaceAllString(str, "")
}

// Envoy doesn't have first class support for case sensitivity or inversion when
// matching against headers. We sidestep the inversion limitation by using an
// inverse regex match. Since Turbine stores regexes for HeaderConstraints,
// we default the Regex flag.
func toEnvoyHeaderMatcher(
	domain tbnapi.Domain,
	redirect tbnapi.Redirect,
) []*envoyroute.HeaderMatcher {
	matchers := []*envoyroute.HeaderMatcher{}
	for _, headerConstraint := range redirect.HeaderConstraints {
		headerValue := headerConstraint.Value

		if headerConstraint.Invert {
			// We can include regex characters in this inversion except
			// for some literals
			strippedValue := strings.Replace(headerValue, ".", "\\.", -1)
			headerValue = fmt.Sprintf(inverseValueMatchTemplate, strippedValue)
		}

		headerMatcher := &envoyroute.HeaderMatcher{
			Name: headerConstraint.Name,
			HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
				RegexMatch: headerValue,
			},
		}

		matchers = append(matchers, headerMatcher)
	}

	if len(matchers) == 0 {
		return nil
	}

	return matchers
}

func valueOrDefault(s string) string {
	if s == "" {
		return "DEFAULT"
	}
	return s
}

func tbnToEnvoyRoutes(
	domainConfig domainConfig,
	objects *poller.Objects,
) ([]envoyroute.Route, error) {
	routes := []envoyroute.Route{}

	for _, routeConfig := range domainConfig.Routes {
		re := routeExploder{domainConfig, routeConfig, objects.ClusterFromKey}
		for _, rv := range re.Explode() {
			match := envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: routeConfig.Route.Path,
				},
				CaseSensitive:   boolValue(false),
				Headers:         mkEnvoyHeaderMatchers(rv.RouteMeta.matches, rv.Methods),
				QueryParameters: mkEnvoyQueryParamMatchers(rv.RouteMeta.matches),
			}

			weightedCluster, err := mkEnvoyWeightedCluster(
				rv.RouteMeta.constraintMeta,
				rv.Destination,
				objects,
				rv.ResponseData,
			)
			if err != nil {
				return nil, err
			}

			rp := routeConfig.Route.RetryPolicy

			action := &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					Priority: defaultRoutingPriority,
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: weightedCluster,
					},
					RequestHeadersToAdd: []*envoycore.HeaderValueOption{
						{
							Header: &envoycore.HeaderValue{
								Key:   headerRouteKey,
								Value: valueOrDefault(string(routeConfig.Route.RouteKey)),
							},
							Append: boolValue(false),
						},
						{
							Header: &envoycore.HeaderValue{
								Key:   headerRuleKey,
								Value: valueOrDefault(string(rv.RuleKey)),
							},
							Append: boolValue(false),
						},
						{
							Header: &envoycore.HeaderValue{
								Key:   headerSharedRulesKey,
								Value: valueOrDefault(rv.SharedRulesName),
							},
							Append: boolValue(false),
						},
					},
					RetryPolicy: &envoyroute.RouteAction_RetryPolicy{
						RetryOn:       rdsRetryOn,
						NumRetries:    uint32Value(uint32(rp.NumRetries)),
						PerTryTimeout: ptr.Duration(time.Duration(rp.PerTryTimeoutMsec) * time.Millisecond),
					},
					Timeout: ptr.Duration(time.Duration(rp.TimeoutMsec) * time.Millisecond),
				},
			}

			routes = append(routes, envoyroute.Route{Match: match, Action: action})
		}
	}

	return routes, nil
}

// Escape percent signs in literal HeaderValueOption data. This is
// required only for literal data appearing in an Envoy API
// HeaderValueOption value.
func escapeLiteralHeaderData(s string) string {
	return strings.Replace(s, "%", "%%", -1)
}

func mkEnvoyResponseHeaders(
	responseData tbnapi.ResponseData,
) ([]*envoycore.HeaderValueOption, error) {
	if responseData.Len() == 0 {
		return nil, nil
	}

	headers := make([]*envoycore.HeaderValueOption, 0, responseData.Len())
	for _, headerDatum := range responseData.Headers {
		var headerValue string
		if headerDatum.ValueIsLiteral {
			headerValue = escapeLiteralHeaderData(escapeMetadata(headerDatum.Value))
		} else {
			metadata, err := json.Marshal([]string{loadBalancerMetadataKey, headerDatum.Value})
			if err != nil {
				return nil, fmt.Errorf(
					"error formatting dynamic header %s: %s",
					headerDatum.Name,
					err.Error(),
				)
			}

			headerValue = fmt.Sprintf(`%%UPSTREAM_METADATA(%s)%%`, string(metadata))
		}

		headers = append(
			headers,
			&envoycore.HeaderValueOption{
				Header: &envoycore.HeaderValue{
					Key:   headerDatum.Name,
					Value: headerValue,
				},
				Append: boolValue(false),
			},
		)
	}

	for _, cookieDatum := range responseData.Cookies {

		var cookieValue string
		if cookieDatum.ValueIsLiteral {
			cookieValue = escapeLiteralHeaderData(escapeMetadata(cookieDatum.Value))
		} else {
			metadataPath := []string{loadBalancerMetadataKey, cookieDatum.Value}
			jsonBytes, err := json.Marshal(&metadataPath)
			if err != nil {
				return nil, err
			}
			cookieValue = fmt.Sprintf("%%UPSTREAM_METADATA(%s)%%", string(jsonBytes))
		}

		parts := make([]string, 5)
		parts = append(
			parts,
			escapeLiteralHeaderData(cookieDatum.Name),
			"=",
			cookieValue,
		)
		if anno := cookieDatum.Annotation(); anno != "" {
			parts = append(parts, "; ", anno)
		}

		headers = append(
			headers,
			&envoycore.HeaderValueOption{
				Header: &envoycore.HeaderValue{
					Key:   "Set-Cookie",
					Value: strings.Join(parts, ""),
				},
				Append: boolValue(true),
			},
		)
	}

	return headers, nil
}

func mkEnvoyWeightedCluster(
	metadata constraintMap,
	allConstraints tbnapi.AllConstraints,
	objects *poller.Objects,
	responseData tbnapi.ResponseData,
) (*envoyroute.WeightedCluster, error) {
	totalWeight := uint32(0)
	weightedClusters := []*envoyroute.WeightedCluster_ClusterWeight{}
	for _, cc := range allConstraints.Light {
		// Not all clusters will be present in the enumeration of
		// wildcard constraints to concrete cluster constraints. Also,
		// on the off chance that we see a negative weight, ignore it.
		constraint, ok := metadata[cc.ConstraintKey]
		if !ok || cc.Weight < 0 {
			continue
		}

		merged := responseData.MergeFrom(constraint.responseData)

		responseHeaders, err := mkEnvoyResponseHeaders(merged)
		if err != nil {
			return nil, err
		}

		weight := uint32(cc.Weight)
		totalWeight += weight
		weightedCluster := &envoyroute.WeightedCluster_ClusterWeight{
			Name:          objects.ClusterFromKey(cc.ClusterKey).Name,
			Weight:        uint32Value(weight),
			MetadataMatch: toEnvoyMetadata(constraint.metadata),
			RequestHeadersToAdd: []*envoycore.HeaderValueOption{
				{
					Header: &envoycore.HeaderValue{
						Key:   headerConstraintKey,
						Value: string(cc.ConstraintKey),
					},
					Append: boolValue(false),
				},
			},
			ResponseHeadersToAdd: responseHeaders,
		}

		weightedClusters = append(weightedClusters, weightedCluster)
	}

	return &envoyroute.WeightedCluster{
		Clusters:    weightedClusters,
		TotalWeight: uint32Value(totalWeight),
	}, nil
}

// Passes through header matchers as either exact or wildcard
// matches. Handles cookie matches by converting them into header
// based matches with a fun regex. Handles request method matches by
// matching against the :method pseudo-header.
func mkEnvoyHeaderMatchers(matches []requestMatch, methods []string) []*envoyroute.HeaderMatcher {
	matchers := []*envoyroute.HeaderMatcher{}

	if len(methods) > 0 {
		// Match behavior of the old nginx implementation.
		if indexof.String(methods, "GET") != indexof.NotFound {
			methods = append(methods, "HEAD")
		}

		var methodMatcher *envoyroute.HeaderMatcher
		if len(methods) == 1 {
			methodMatcher = &envoyroute.HeaderMatcher{
				Name: envoyMethodHeader,
				HeaderMatchSpecifier: &envoyroute.HeaderMatcher_ExactMatch{
					ExactMatch: methods[0],
				},
			}
		} else {
			methodMatcher = &envoyroute.HeaderMatcher{
				Name: envoyMethodHeader,
				HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
					RegexMatch: fmt.Sprintf("^(%s)$", strings.Join(methods, "|")),
				},
			}
		}
		matchers = append(matchers, methodMatcher)
	}

	for _, hmm := range matches {
		switch {
		case hmm.matchKind == tbnapi.HeaderMatchKind:
			headerExpr, isRegex := headerMatcherForMetadata(hmm.metadatum.Value)
			headerMatcher := &envoyroute.HeaderMatcher{Name: hmm.metadatum.Key}

			if isRegex {
				headerMatcher.HeaderMatchSpecifier = &envoyroute.HeaderMatcher_RegexMatch{
					RegexMatch: headerExpr,
				}
			} else {
				headerMatcher.HeaderMatchSpecifier = &envoyroute.HeaderMatcher_ExactMatch{
					ExactMatch: headerExpr,
				}
			}
			matchers = append(matchers, headerMatcher)

		case hmm.matchKind == tbnapi.CookieMatchKind && hmm.metadatum.Value == "":
			headerMatcher := &envoyroute.HeaderMatcher{
				Name: cookieHeaderName,
				HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
					fmt.Sprintf(
						wildcardCookieMatchTemplate,
						regexp.QuoteMeta(hmm.metadatum.Key),
					),
				},
			}
			matchers = append(matchers, headerMatcher)

		case hmm.matchKind == tbnapi.CookieMatchKind && hmm.metadatum.Value != "":
			headerMatcher := &envoyroute.HeaderMatcher{
				Name: cookieHeaderName,
				HeaderMatchSpecifier: &envoyroute.HeaderMatcher_RegexMatch{
					RegexMatch: fmt.Sprintf(
						exactCookieMatchTemplate,
						regexp.QuoteMeta(hmm.metadatum.Key),
						cookieMatcherForMetadata(hmm.metadatum.Value),
					),
				},
			}
			matchers = append(matchers, headerMatcher)

		default:
			continue
		}
	}

	if len(matchers) == 0 {
		return nil
	}

	// N.B. To ensure consistent output for tests
	sort.Slice(matchers, func(i, j int) bool {
		if matchers[i].Name < matchers[j].Name {
			return true
		}

		if matchers[i].Name > matchers[j].Name {
			return false
		}

		if matchers[i].GetExactMatch() < matchers[j].GetExactMatch() {
			return true
		}

		if matchers[i].GetExactMatch() > matchers[j].GetExactMatch() {
			return false
		}

		return matchers[i].GetRegexMatch() < matchers[j].GetRegexMatch()
	})

	return matchers
}

// Generates QueryParameterMatchers as necessary.
func mkEnvoyQueryParamMatchers(
	matches []requestMatch,
) []*envoyroute.QueryParameterMatcher {
	matchers := []*envoyroute.QueryParameterMatcher{}
	for _, qpm := range matches {
		switch {
		case qpm.matchKind == tbnapi.QueryMatchKind:
			queryMatcher := &envoyroute.QueryParameterMatcher{
				Name:  qpm.metadatum.Key,
				Value: queryMatcherForMetadata(qpm.metadatum.Value),
				Regex: boolValue(false),
			}
			matchers = append(matchers, queryMatcher)

		default:
			continue
		}
	}

	if len(matchers) == 0 {
		return nil
	}

	// N.B. To ensure consistent output for tests
	sort.Slice(matchers, func(i, j int) bool {
		if matchers[i].Name < matchers[j].Name {
			return true
		}

		if matchers[i].Name > matchers[j].Name {
			return false
		}

		return matchers[i].Value < matchers[j].Value
	})

	return matchers
}
