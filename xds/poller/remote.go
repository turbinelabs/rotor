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

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"fmt"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
)

// Remote is an adapter to the Turbine API for obtaining
// ConfigObjects for a Proxy Config.
type Remote interface {
	Objects(proxyKey api.ProxyKey) (*Objects, error)

	// ObjectsWithOverrides returns the *Objects for a given ProxyKey. If a
	// function exists in the map corresponding to the key of an object (cast as a
	// string) that exists in the *Objects being constructed, that function will
	// be invoked on the *Objects before proceeding to the next step of the object
	// graph walk. If a proxy is being created then newProxy will be used as the
	// starting point for the validation.
	ObjectsWithOverrides(
		proxyKey api.ProxyKey,
		overrides map[string]ObjectsUpdateFn,
		newProxy *api.Proxy,
	) (*Objects, error)
}

// NewRemote returns a Remote backed by the given service.All.
func NewRemote(svc service.All) Remote {
	sr := svc.SharedRules()
	return NewRemoteFromFns(
		svc.Zone().Get,
		svc.Proxy().Get,
		svc.Domain().Index,
		svc.Listener().Index,
		svc.Route().Index,
		sr.Index,
		sr.Get,
		svc.Cluster().Index,
	)
}

// NewRemoteFromFns returns a Remote backed by the specified function pointers.
func NewRemoteFromFns(
	zoneGet func(api.ZoneKey) (api.Zone, error),
	proxyGet func(api.ProxyKey) (api.Proxy, error),
	domainIndex func(...service.DomainFilter) (api.Domains, error),
	listenerIndex func(...service.ListenerFilter) (api.Listeners, error),
	routeIndex func(...service.RouteFilter) (api.Routes, error),
	sharedRulesIndex func(...service.SharedRulesFilter) (api.SharedRulesSlice, error),
	sharedRulesGet func(api.SharedRulesKey) (api.SharedRules, error),
	clusterIndex func(...service.ClusterFilter) (api.Clusters, error),
) Remote {
	return svcRemote{
		zoneGet,
		proxyGet,
		domainIndex,
		listenerIndex,
		routeIndex,
		sharedRulesIndex,
		sharedRulesGet,
		clusterIndex,
	}
}

type svcRemote struct {
	zoneGet          func(api.ZoneKey) (api.Zone, error)
	proxyGet         func(api.ProxyKey) (api.Proxy, error)
	domainIndex      func(...service.DomainFilter) (api.Domains, error)
	listenerIndex    func(...service.ListenerFilter) (api.Listeners, error)
	routeIndex       func(...service.RouteFilter) (api.Routes, error)
	sharedRulesIndex func(...service.SharedRulesFilter) (api.SharedRulesSlice, error)
	sharedRulesGet   func(api.SharedRulesKey) (api.SharedRules, error)
	clusterIndex     func(...service.ClusterFilter) (api.Clusters, error)
}

func (r svcRemote) Objects(proxyKey api.ProxyKey) (*Objects, error) {
	return r.ObjectsWithOverrides(proxyKey, nil, nil)
}

func (r svcRemote) ObjectsWithOverrides(
	proxyKey api.ProxyKey,
	overrides map[string]ObjectsUpdateFn,
	newProxy *api.Proxy,
) (*Objects, error) {
	if newProxy != nil && newProxy.ProxyKey != proxyKey {
		return nil, fmt.Errorf("new proxy does not match proxy key to be validated")
	}

	proxy, err := r.proxyGet(proxyKey)
	if err != nil {
		return nil, err
	}

	if proxy.ProxyKey != "" && newProxy != nil {
		return nil, fmt.Errorf("called with new Proxy while using key for existing proxy")
	}

	zoneKey := proxy.ZoneKey

	co := &Objects{}
	if newProxy != nil {
		proxy = *newProxy
	}

	if len(proxy.ZoneKey) > 0 {
		zoneKey = proxy.ZoneKey
	}

	co.Proxy = proxy
	co.Checksum = proxy.Checksum

	if fn := overrides[string(proxyKey)]; fn != nil {
		console.Info().Printf("Applying object override on Proxy %v\n", proxyKey)
		fn(co)
	}
	proxy = co.Proxy

	co.Zone, err = r.zoneGet(zoneKey)
	if err != nil {
		return nil, err
	}

	domains, err := r.getDomains(proxy.ZoneKey, proxy.DomainKeys)
	if err != nil {
		return nil, err
	}

	co.Domains = domains
	if len(overrides) != 0 {
		for _, d := range co.Domains {
			if fn := overrides[string(d.DomainKey)]; fn != nil {
				console.Info().Printf("Applying object override on Domain %v\n", d.DomainKey)
				fn(co)
			}
		}
	}
	domains = co.Domains

	listeners, err := r.getListeners(proxy.ZoneKey, proxy.ListenerKeys)
	if err != nil {
		return nil, err
	}

	co.Listeners = listeners
	if len(overrides) != 0 {
		for _, d := range co.Listeners {
			if fn := overrides[string(d.ListenerKey)]; fn != nil {
				console.Info().Printf("Applying object override on Listener %v\n", d.ListenerKey)
				fn(co)
			}
		}
	}
	listeners = co.Listeners

	routes, err := r.getRoutes(proxy.ZoneKey, proxy.DomainKeys)
	if err != nil {
		return nil, err
	}

	co.Routes = routes
	if len(overrides) != 0 {
		for _, r := range routes {
			if fn := overrides[string(r.RouteKey)]; fn != nil {
				console.Info().Printf("Applying object override on Route %v\n", r.RouteKey)
				fn(co)
			}
		}
	}
	routes = co.Routes

	sharedRules, err := r.getSharedRules(proxy.ZoneKey, routes)
	if err != nil {
		return nil, err
	}
	co.SharedRules = sharedRules
	if len(overrides) != 0 {
		for _, sr := range sharedRules {
			if fn := overrides[string(sr.SharedRulesKey)]; fn != nil {
				console.Info().Printf("Applying object override on SharedRules %v\n", sr.SharedRulesKey)
				fn(co)
			}
		}
	}
	sharedRules = co.SharedRules

	clusters, err := r.getClusters(proxy.ZoneKey, sharedRules, routes)
	if err != nil {
		return nil, err
	}
	co.Clusters = clusters

	if len(overrides) != 0 {
		for _, c := range clusters {
			if fn := overrides[string(c.ClusterKey)]; fn != nil {
				console.Info().Printf("Applying object override on Cluster %v\n", c.ClusterKey)
				fn(co)
			}
		}
	}
	clusters = co.Clusters

	err = co.CheckIntegrity()
	if err != nil {
		return nil, err
	}

	return co, nil
}

func (r svcRemote) getDomains(zoneKey api.ZoneKey, keys []api.DomainKey) ([]api.Domain, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	filters := make([]service.DomainFilter, len(keys))
	for i, key := range keys {
		filters[i] = service.DomainFilter{ZoneKey: zoneKey, DomainKey: key}
	}

	if len(filters) == 0 {
		return nil, nil
	}
	return r.domainIndex(filters...)
}

func (r svcRemote) getListeners(zoneKey api.ZoneKey, keys []api.ListenerKey) ([]api.Listener, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	filters := make([]service.ListenerFilter, len(keys))
	for i, key := range keys {
		filters[i] = service.ListenerFilter{ZoneKey: zoneKey, ListenerKey: key}
	}

	if len(filters) == 0 {
		return nil, nil
	}
	return r.listenerIndex(filters...)
}

func (r svcRemote) getRoutes(zoneKey api.ZoneKey, keys []api.DomainKey) ([]api.Route, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	filters := make([]service.RouteFilter, len(keys))
	for i, key := range keys {
		filters[i] = service.RouteFilter{ZoneKey: zoneKey, DomainKey: key}
	}
	if len(filters) == 0 {
		return nil, nil
	}
	return r.routeIndex(filters...)
}

func (r svcRemote) getSharedRules(
	zoneKey api.ZoneKey,
	routes []api.Route,
) ([]api.SharedRules, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	filters := []service.SharedRulesFilter{}
	keys := map[api.SharedRulesKey]bool{}
	for _, r := range routes {
		if keys[r.SharedRulesKey] {
			continue
		}
		key := r.SharedRulesKey
		keys[key] = true
		filters = append(filters, service.SharedRulesFilter{ZoneKey: zoneKey, SharedRulesKey: key})
	}
	if len(filters) == 0 {
		return nil, nil
	}
	return r.sharedRulesIndex(filters...)
}

func (r svcRemote) getClusters(
	zoneKey api.ZoneKey,
	sharedRules []api.SharedRules,
	routes []api.Route,
) ([]api.Cluster, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	srMap := map[api.SharedRulesKey]api.SharedRules{}
	for _, sr := range sharedRules {
		srMap[sr.SharedRulesKey] = sr
	}

	exists := map[api.ClusterKey]bool{}
	cfs := []service.ClusterFilter{}

	// visit function appends the cluster key if not already seen
	visit := func(cc api.ClusterConstraint, r *api.Rule) {
		ck := cc.ClusterKey
		if !exists[ck] {
			exists[ck] = true
			cfs = append(cfs, service.ClusterFilter{ClusterKey: ck, ZoneKey: zoneKey})
		}
	}

	walkedSRs := map[api.SharedRulesKey]bool{}

	for _, route := range routes {
		srk := route.SharedRulesKey
		sr, hasSR := srMap[srk]

		if !hasSR {
			return nil, fmt.Errorf(
				`SharedRules object "%v" referenced by Route "%v" but not available`, srk, route.RouteKey)
		}

		// don't walk again if we already did
		if !walkedSRs[srk] {
			walkedSRs[srk] = true
			api.WalkConstraintsFromSharedRules(sr, visit)
		}

		api.WalkConstraintsFromRules(route.Rules, visit)
	}

	return r.clusterIndex(cfs...)
}
