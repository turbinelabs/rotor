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
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/codec"
)

// ObjectsUpdateFn is a type alias for a function that operates on an *Objects
type ObjectsUpdateFn func(*Objects)

// Objects represents the api.Clusters, api.Domains, and api.Routes needed
// to construct a tbnproxy config file using a template.
type Objects struct {
	Zone        api.Zone          `json:"zone"`
	Proxy       api.Proxy         `json:"proxy"`
	Clusters    []api.Cluster     `json:"clusters"`
	Domains     api.Domains       `json:"domains"`
	Routes      []api.Route       `json:"routes"`
	SharedRules []api.SharedRules `json:"shared_rules"`
	Checksum    api.Checksum      `json:"checksum"`
	meta        *objectsMeta
}

type objectsMeta struct {
	clusterKeysToCluster map[api.ClusterKey]api.Cluster
	domainKeysToName     map[api.DomainKey]string
	sharedRulesMap       map[api.SharedRulesKey]api.SharedRules
	domainsByPort        map[int]api.Domains
	routesByDomainKey    map[api.DomainKey]api.Routes
	subsetsByClusterKey  map[api.ClusterKey][][]string
	hash                 string
}

// TaggedRule represents a rule that has been aggregated from the Rules of
// a Route and the Rules of its SharedRules.
type TaggedRule struct {
	api.Rule
	// SharedRuleName is empty if the Rule came from a Route
	SharedRuleName string
}

// TerribleHash returns a string that can be used for
// equality comparison between two Objects. It is computed
// once, and will not track changes to the Objects.
func (co *Objects) TerribleHash() string {
	co.maybeInit()
	return co.meta.hash
}

// Init forces all metadata to be recomputed
func (co *Objects) Init() {
	co.init()
}

func (co *Objects) maybeInit() {
	if co.meta != nil {
		return
	}

	co.init()
}

// init maps for Cluster and Domain lookups
func (co *Objects) init() {
	meta := &objectsMeta{}

	h := md5.New()
	codec.NewJson().Encode(co, h)
	meta.hash = base64.StdEncoding.EncodeToString(h.Sum(nil))

	subsetMap := map[api.ClusterKey]map[string][]string{}

	visit := func(cc api.ClusterConstraint, r *api.Rule) {
		subsets := []string{}
		if r != nil {
			for _, m := range r.Matches {
				if m.To.Key != "" {
					subsets = append(subsets, m.To.Key)
				}
			}
		}
		for _, m := range cc.Metadata {
			subsets = append(subsets, m.Key)
		}
		if len(subsets) == 0 {
			return
		}

		sort.Strings(subsets)
		subsetKey := strings.Join(subsets, "%@%")
		if subsetMap[cc.ClusterKey] == nil {
			subsetMap[cc.ClusterKey] = map[string][]string{}
		}
		subsetMap[cc.ClusterKey][subsetKey] = subsets
	}

	meta.sharedRulesMap = map[api.SharedRulesKey]api.SharedRules{}
	for _, sr := range co.SharedRules {
		meta.sharedRulesMap[sr.SharedRulesKey] = sr
		api.WalkConstraintsFromSharedRules(sr, visit)
	}

	meta.clusterKeysToCluster = map[api.ClusterKey]api.Cluster{}
	for _, c := range co.Clusters {
		meta.clusterKeysToCluster[c.ClusterKey] = c
	}

	meta.domainKeysToName = make(map[api.DomainKey]string, len(co.Domains))
	meta.domainsByPort = make(map[int]api.Domains)
	for _, d := range co.Domains {
		meta.domainKeysToName[d.DomainKey] = d.Name
		meta.domainsByPort[d.Port] = append(meta.domainsByPort[d.Port], d)
	}

	meta.routesByDomainKey = make(map[api.DomainKey]api.Routes, len(co.Domains))
	for _, rt := range co.Routes {
		meta.routesByDomainKey[rt.DomainKey] = append(meta.routesByDomainKey[rt.DomainKey], rt)
		api.WalkConstraintsFromRules(rt.Rules, visit)
	}

	meta.subsetsByClusterKey = make(map[api.ClusterKey][][]string, len(subsetMap))
	for ck, sMap := range subsetMap {
		// sort keys to force stable ordering. yuck.
		sMapKeys := make([]string, 0, len(sMap))
		for k := range sMap {
			sMapKeys = append(sMapKeys, k)
		}
		sort.Strings(sMapKeys)

		subsets := make([][]string, 0, len(sMap))
		for _, k := range sMapKeys {
			subsets = append(subsets, sMap[k])
		}
		meta.subsetsByClusterKey[ck] = subsets
	}

	co.meta = meta
}

func (co *Objects) checkConstraints(cs []api.ClusterConstraint, r string) error {
	for _, c := range cs {
		if _, ok := co.meta.clusterKeysToCluster[c.ClusterKey]; !ok {
			return fmt.Errorf(
				"unknown cluster %s referenced in %s", c.ClusterKey, r)
		}
	}
	return nil
}

func (co *Objects) checkAllConstraints(c api.AllConstraints, r string) error {
	if err := co.checkConstraints(c.Light, r); err != nil {
		return err
	}
	if err := co.checkConstraints(c.Dark, r); err != nil {
		return err
	}
	if err := co.checkConstraints(c.Tap, r); err != nil {
		return err
	}
	return nil
}

// CheckIntegrity makes sure that no non-existant Domains or Clusters are
// referenced in co.Routes or co.SharedRules. Assumes no change to the Objects
// after initial construction.
func (co *Objects) CheckIntegrity() error {
	co.maybeInit()

	for _, dk := range co.Proxy.DomainKeys {
		if _, ok := co.meta.domainKeysToName[dk]; !ok {
			return fmt.Errorf(
				"unknown domain %v referenced in proxy %v", dk, co.Proxy.ProxyKey)
		}
	}

	for _, sr := range co.SharedRules {
		errLoc := "shared_rules " + string(sr.SharedRulesKey)
		if err := co.checkAllConstraints(sr.Default, errLoc); err != nil {
			return err
		}

		for _, srRule := range sr.Rules {
			curErrLoc := errLoc + " rule " + string(srRule.RuleKey)
			if err := co.checkAllConstraints(srRule.Constraints, curErrLoc); err != nil {
				return err
			}
		}
	}

	for _, r := range co.Routes {
		if _, ok := co.meta.sharedRulesMap[r.SharedRulesKey]; !ok {
			return fmt.Errorf(
				"unknown shared_rules %s referenced in route %s",
				r.SharedRulesKey,
				r.DomainKey,
			)
		}

		for _, ru := range r.Rules {
			if err := co.checkAllConstraints(ru.Constraints, "route "+r.Path); err != nil {
				return err
			}
		}
		if _, ok := co.meta.domainKeysToName[r.DomainKey]; !ok {
			return fmt.Errorf(
				"unknown domain %s referenced in route %s", r.DomainKey, r.Path)
		}
	}
	return nil
}

// ClusterFromKey returns the Cluster object associated with the specified
// ClusterKey. This is only for use by a config template and should not be
// used to determine if a given ClusterKey is present in the Clusters slice.
func (co *Objects) ClusterFromKey(key api.ClusterKey) api.Cluster {
	co.maybeInit()
	return co.meta.clusterKeysToCluster[key]
}

// SharedRulesFromKey returns the SharedRules object associated with a given
// key. This is only used by config templates. Panics if the SharedRulesKey does
// not resolve to a valid SharedRules object.
func (co *Objects) SharedRulesFromKey(key api.SharedRulesKey) api.SharedRules {
	co.maybeInit()
	return co.meta.sharedRulesMap[key]
}

// DomainsPerPort returns the list of Domains which have the given port
func (co *Objects) DomainsPerPort(port int) api.Domains {
	co.maybeInit()

	domains, ok := co.meta.domainsByPort[port]
	if !ok {
		return nil
	}

	result := make(api.Domains, len(domains))
	copy(result, domains)
	return result
}

// RoutesPerDomain returns all routes associated with the given DomainKey
func (co *Objects) RoutesPerDomain(domainKey api.DomainKey) api.Routes {
	co.maybeInit()

	routes, ok := co.meta.routesByDomainKey[domainKey]
	if !ok {
		return nil
	}

	result := make(api.Routes, len(routes))
	copy(result, routes)
	return result
}

// AllPorts returns the aggregate list of ports derived from the Domains
func (co *Objects) AllPorts() []int {
	co.maybeInit()

	if len(co.meta.domainsByPort) == 0 {
		return nil
	}

	ports := make([]int, 0, len(co.meta.domainsByPort))
	for port := range co.meta.domainsByPort {
		ports = append(ports, port)
	}

	return ports
}

// SubsetsPerCluster returns a list of sets of metadata keys corresponding
// to distinct Match/ClusterConstraint pairs for the given Cluster.
func (co *Objects) SubsetsPerCluster(clusterKey api.ClusterKey) [][]string {
	co.maybeInit()

	subsets, ok := co.meta.subsetsByClusterKey[clusterKey]
	if !ok {
		return nil
	}

	result := make([][]string, len(subsets))
	copy(result, subsets)
	return result
}

// AggregateRules returns a computed rule set for a given route that combines
// the Rules inherited from the referenced SharedRules and the Route-specific
// rules defined locally.
func (co *Objects) AggregateRules(rt api.Route) []TaggedRule {
	co.maybeInit()

	srk := rt.SharedRulesKey
	sr := co.meta.sharedRulesMap[srk]
	length := len(sr.Rules) + len(rt.Rules)
	if length == 0 {
		return nil
	}

	rules := make([]TaggedRule, 0, length)
	for _, r := range rt.Rules {
		rules = append(rules, TaggedRule{r, ""})
	}
	for _, r := range sr.Rules {
		rules = append(rules, TaggedRule{r, sr.Name})
	}

	return rules
}
