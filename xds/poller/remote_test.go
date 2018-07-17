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
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/test/assert"
)

type remoteMocks struct {
	Remote
	cluster     *service.MockCluster
	domain      *service.MockDomain
	listener    *service.MockListener
	proxy       *service.MockProxy
	route       *service.MockRoute
	sharedRules *service.MockSharedRules
	zone        *service.MockZone
	finish      func()
}

func mkRemoteMocks(t testing.TB) remoteMocks {
	ctrl := gomock.NewController(assert.Tracing(t))
	mocks := remoteMocks{
		zone:        service.NewMockZone(ctrl),
		cluster:     service.NewMockCluster(ctrl),
		domain:      service.NewMockDomain(ctrl),
		listener:    service.NewMockListener(ctrl),
		proxy:       service.NewMockProxy(ctrl),
		route:       service.NewMockRoute(ctrl),
		sharedRules: service.NewMockSharedRules(ctrl),
		finish:      func() { ctrl.Finish() },
	}

	remote := NewRemoteFromFns(
		mocks.zone.Get,
		mocks.proxy.Get,
		mocks.domain.Index,
		mocks.listener.Index,
		mocks.route.Index,
		mocks.sharedRules.Index,
		mocks.sharedRules.Get,
		mocks.cluster.Index,
	)

	mocks.Remote = remote
	return mocks
}

var (
	errTestRemoteGetProxy       = errors.New("proxy.get")
	errTestRemoteGetZone        = errors.New("zone.get")
	errTestRemoteGetDomains     = errors.New("domain.get")
	errTestRemoteGetRoutes      = errors.New("routes.get")
	errTestRemoteGetSharedRules = errors.New("shared_rules.get")
	errTestRemoteGetClusters    = errors.New("clusters.get")

	testRemoteZoneKey = api.ZoneKey("Z0")
)

type remoteObjectsTestCase struct {
	getProxyErr       bool
	getZoneErr        bool
	getDomainsErr     bool
	emptyDomains      bool
	getSharedRulesErr bool
	emptyRoutes       bool
	getRoutesErr      bool
	getClustersErr    bool
	checkIntegrityErr bool
	wantErr           error
}

func (tc remoteObjectsTestCase) run(t testing.TB) {
	objects := MkFixtureObjects()
	if tc.emptyDomains {
		objects.Proxy.DomainKeys = nil
		objects.Domains = nil
		objects.Routes = nil
		objects.SharedRules = nil
		objects.Clusters = nil
	}
	if tc.emptyRoutes {
		objects.Routes = nil
		objects.SharedRules = nil
		objects.Clusters = nil
	}
	assert.Nil(t, objects.CheckIntegrity())

	var want *Objects
	if tc.wantErr == nil {
		// copy because we want fresh meta
		want = &Objects{
			Zone:        objects.Zone,
			Proxy:       objects.Proxy,
			Clusters:    objects.Clusters,
			Domains:     objects.Domains,
			Routes:      objects.Routes,
			SharedRules: objects.SharedRules,
			Checksum:    objects.Checksum,
		}
	}
	dKeys := make([]api.DomainKey, len(objects.Domains))
	dFilters := make([]interface{}, len(objects.Domains))
	rFilters := make([]interface{}, len(objects.Domains))
	srFilters := make([]interface{}, len(objects.SharedRules))

	for i, d := range objects.Domains {
		dKeys[i] = d.DomainKey
		dFilters[i] = service.DomainFilter{DomainKey: d.DomainKey, ZoneKey: testRemoteZoneKey}
		rFilters[i] = service.RouteFilter{DomainKey: d.DomainKey, ZoneKey: testRemoteZoneKey}
	}

	for i, sr := range objects.SharedRules {
		srFilters[i] = service.SharedRulesFilter{SharedRulesKey: sr.SharedRulesKey, ZoneKey: testRemoteZoneKey}
	}

	proxy := api.Proxy{
		ProxyKey:   "PK-1",
		Checksum:   objects.Checksum,
		DomainKeys: dKeys,
		ZoneKey:    testRemoteZoneKey,
	}

	mocks := mkRemoteMocks(t)
	calls := make([]*gomock.Call, 0)

	defer func() {
		if want != nil {
			want.Init()
		}
		gomock.InOrder(calls...)
		got, gotErr := mocks.Objects(proxy.ProxyKey)
		assert.DeepEqual(t, gotErr, tc.wantErr)
		assert.DeepEqual(t, got, want)
		mocks.finish()
	}()

	if tc.getProxyErr {
		calls = append(
			calls,
			mocks.proxy.EXPECT().Get(proxy.ProxyKey).Return(api.Proxy{}, errTestRemoteGetProxy),
		)
		return
	}

	calls = append(calls, mocks.proxy.EXPECT().Get(proxy.ProxyKey).Return(proxy, nil))
	if want != nil {
		want.Proxy = proxy
	}

	if tc.getZoneErr {
		calls = append(
			calls,
			mocks.zone.EXPECT().Get(proxy.ZoneKey).Return(api.Zone{}, errTestRemoteGetZone),
		)
		return
	}

	calls = append(calls, mocks.zone.EXPECT().Get(proxy.ZoneKey).Return(objects.Zone, nil))

	if tc.emptyDomains {
		return
	}

	if tc.getDomainsErr {
		calls = append(calls, mocks.domain.EXPECT().Index(dFilters...).Return(nil, errTestRemoteGetDomains))
		return
	}

	calls = append(
		calls,
		mocks.domain.EXPECT().Index(dFilters...).Return(objects.Domains, nil),
	)

	if tc.getRoutesErr {
		calls = append(calls, mocks.route.EXPECT().Index(rFilters...).Return(nil, errTestRemoteGetRoutes))
		return
	}

	calls = append(
		calls,
		mocks.route.EXPECT().Index(rFilters...).Return(objects.Routes, nil),
	)

	if tc.emptyRoutes {
		return
	}

	if tc.getSharedRulesErr {
		calls = append(
			calls,
			mocks.sharedRules.EXPECT().Index(srFilters...).Return(nil, errTestRemoteGetSharedRules),
		)
		return
	}
	calls = append(calls, mocks.sharedRules.EXPECT().Index(srFilters...).Return(objects.SharedRules, nil))

	cFilters := make([]interface{}, len(objects.Clusters))
	for i, c := range objects.Clusters {
		cFilters[i] = service.ClusterFilter{ClusterKey: c.ClusterKey, ZoneKey: testRemoteZoneKey}
	}

	if tc.getClustersErr {
		calls = append(calls, mocks.cluster.EXPECT().Index(cFilters...).Return(nil, errTestRemoteGetClusters))
		return
	}

	clusters := objects.Clusters
	if tc.checkIntegrityErr {
		clusters = make([]api.Cluster, len(objects.Clusters))
		copy(clusters, objects.Clusters)
		clusters[0].ClusterKey = api.ClusterKey("bogus")
	}

	calls = append(calls, mocks.cluster.EXPECT().Index(cFilters...).Return(clusters, nil))
}

func TestRemoteObjectsGetProxyErr(t *testing.T) {
	remoteObjectsTestCase{
		getProxyErr: true,
		wantErr:     errTestRemoteGetProxy,
	}.run(t)
}

func TestRemoteObjectsGetZoneErr(t *testing.T) {
	remoteObjectsTestCase{
		getZoneErr: true,
		wantErr:    errTestRemoteGetZone,
	}.run(t)
}

func TestRemoteObjectsGetDomainsErr(t *testing.T) {
	remoteObjectsTestCase{
		getDomainsErr: true,
		wantErr:       errTestRemoteGetDomains,
	}.run(t)
}

func TestRemoteObjectsEmptyDomains(t *testing.T) {
	remoteObjectsTestCase{
		emptyDomains: true,
	}.run(t)
}

func TestRemoteObjectsGetRoutesErr(t *testing.T) {
	remoteObjectsTestCase{
		getRoutesErr: true,
		wantErr:      errTestRemoteGetRoutes,
	}.run(t)
}

func TestRemoteObjectsEmptyRoutes(t *testing.T) {
	remoteObjectsTestCase{
		emptyRoutes: true,
	}.run(t)
}

func TestRemoteObjectsGetSharedRulesErr(t *testing.T) {
	remoteObjectsTestCase{
		getSharedRulesErr: true,
		wantErr:           errTestRemoteGetSharedRules,
	}.run(t)
}

func TestRemoteObjectsGetClustersErr(t *testing.T) {
	remoteObjectsTestCase{
		getClustersErr: true,
		wantErr:        errTestRemoteGetClusters,
	}.run(t)
}

func TestRemoteObjectsCheckIntegrityErr(t *testing.T) {
	remoteObjectsTestCase{
		checkIntegrityErr: true,
		wantErr:           errors.New("unknown cluster C0 referenced in shared_rules SRK-0"),
	}.run(t)
}

func TestRemoteObjectsNewProxy(t *testing.T) {
	mocks := mkRemoteMocks(t)
	defer mocks.finish()

	objects := MkFixtureObjects()

	zk := testRemoteZoneKey
	newProxy := api.Proxy{
		ProxyKey: "PK-NEW",
		ZoneKey:  zk,
	}

	calls := []*gomock.Call{}
	addCall := func(cs ...*gomock.Call) {
		calls = append(calls, cs...)
	}

	newProxy.DomainKeys = []api.DomainKey{"D0", "D1"}

	d0 := api.Domain{DomainKey: "D0"}
	d1 := api.Domain{DomainKey: "D1"}
	z := api.Zone{ZoneKey: zk}

	addCall(
		mocks.proxy.EXPECT().Get(api.ProxyKey("PK-NEW")).Return(api.Proxy{}, nil),
		mocks.zone.EXPECT().Get(zk).Return(z, nil),
		mocks.domain.EXPECT().Index(
			service.DomainFilter{ZoneKey: zk, DomainKey: "D0"},
			service.DomainFilter{ZoneKey: zk, DomainKey: "D1"},
		).Return([]api.Domain{d0, d1}, nil),
		mocks.route.EXPECT().Index(
			service.RouteFilter{ZoneKey: zk, DomainKey: "D0"},
			service.RouteFilter{ZoneKey: zk, DomainKey: "D1"},
		).Return(nil, nil),
	)

	objects.Proxy = newProxy
	objects.Domains = []api.Domain{d0, d1}
	objects.Routes = nil
	objects.SharedRules = nil

	gomock.InOrder(calls...)

	co, err := mocks.ObjectsWithOverrides("PK-NEW", nil, &newProxy)
	assert.Nil(t, err)
	assert.DeepEqual(t, co.Proxy, objects.Proxy)
	assert.DeepEqual(t, co.Domains, objects.Domains)
	assert.DeepEqual(t, co.Routes, objects.Routes)
	assert.DeepEqual(t, co.SharedRules, objects.SharedRules)
}

func TestObjectsNewProxyKeyMismatch(t *testing.T) {
	mocks := mkRemoteMocks(t)
	defer mocks.finish()

	objects := MkFixtureObjects()

	zk := testRemoteZoneKey
	newProxy := api.Proxy{
		ProxyKey: "PK-NEW",
		ZoneKey:  zk,
	}

	objects.Proxy = newProxy

	co, err := mocks.ObjectsWithOverrides("PK-DIFF", nil, &newProxy)
	assert.ErrorContains(t, err, "new proxy does not match proxy key")
	assert.Nil(t, co)
}

func TestObjectsBothNewProxyOldProxy(t *testing.T) {
	mocks := mkRemoteMocks(t)
	defer mocks.finish()

	zk := testRemoteZoneKey
	newProxy := api.Proxy{
		ProxyKey: "PK-NEW",
		ZoneKey:  zk,
	}

	calls := []*gomock.Call{}
	addCall := func(cs ...*gomock.Call) {
		calls = append(calls, cs...)
	}

	newProxy.DomainKeys = []api.DomainKey{"D0", "D1"}

	addCall(mocks.proxy.EXPECT().Get(api.ProxyKey("PK-NEW")).Return(api.Proxy{ProxyKey: "PK-NEW"}, nil))

	gomock.InOrder(calls...)

	co, err := mocks.ObjectsWithOverrides("PK-NEW", nil, &newProxy)
	assert.ErrorContains(t, err, "called with new Proxy while using key for existing proxy")
	assert.Nil(t, co)
}

func overlayTest(
	t *testing.T,
	proxyOverlay map[string]ObjectsUpdateFn,
	wantDomainKeys []api.DomainKey,
	domainOverlay map[string]ObjectsUpdateFn,
	routeOverlay map[string]ObjectsUpdateFn,
	wantSharedRulesKeys []api.SharedRulesKey,
	sharedRulesOverlay map[string]ObjectsUpdateFn,
	wantClusterKeys []api.ClusterKey,
	clusterOverlay map[string]ObjectsUpdateFn,
	errMsg string,
) {
	mocks := mkRemoteMocks(t)
	defer mocks.finish()

	objects := MkFixtureObjects()
	tgtObjects := MkFixtureObjects()

	calls := []*gomock.Call{}
	addCall := func(cs ...*gomock.Call) {
		calls = append(calls, cs...)
	}

	proxy := api.Proxy{
		ProxyKey: "PK-1",
		ZoneKey:  testRemoteZoneKey,
	}
	zk := proxy.ZoneKey
	zone := api.Zone{
		ZoneKey: zk,
		Name:    "that-zone",
	}

	for _, d := range objects.Domains {
		proxy.DomainKeys = append(proxy.DomainKeys, d.DomainKey)
	}
	objects.Proxy = proxy
	tgtObjects.Proxy = objects.Proxy

	overrides := map[string]ObjectsUpdateFn{}
	overlay := []map[string]ObjectsUpdateFn{
		proxyOverlay, domainOverlay, routeOverlay, sharedRulesOverlay, clusterOverlay}
	for _, m := range overlay {
		for k, v := range m {
			overrides[k] = v
		}
	}

	// getProxy
	addCall(
		mocks.proxy.EXPECT().Get(proxy.ProxyKey).Return(proxy, nil),
		mocks.zone.EXPECT().Get(zk).Return(zone, nil),
	)

	for _, fn := range proxyOverlay {
		fn(tgtObjects)
	}

	if tgtObjects.Proxy.Equals(api.Proxy{}) {
		// if a proxy is getting deleted none of the other object should get filled out

		tgtObjects.Clusters = nil
		tgtObjects.Domains = nil
		tgtObjects.Routes = nil
		tgtObjects.SharedRules = nil
		tgtObjects.Checksum = api.Checksum{}
	} else {
		// Only expect the rest of the function to run if the proxy was not deleted

		// getDomains
		if len(wantDomainKeys) != 0 {
			dFilters := []interface{}{}
			for _, k := range wantDomainKeys {
				dFilters = append(dFilters, service.DomainFilter{ZoneKey: zk, DomainKey: k})
			}
			addCall(mocks.domain.EXPECT().Index(dFilters...).Return(objects.Domains, nil))
		} else {
			addCall(mocks.domain.EXPECT().Index(gomock.Any(), gomock.Any()).Return(objects.Domains, nil))
		}

		for _, fn := range domainOverlay {
			fn(tgtObjects)
		}

		// getRoutes
		if len(wantDomainKeys) != 0 {
			rFilters := []interface{}{}
			for _, k := range wantDomainKeys {
				rFilters = append(rFilters, service.RouteFilter{ZoneKey: zk, DomainKey: k})
			}
			addCall(mocks.route.EXPECT().Index(rFilters...).Return(objects.Routes, nil))
		} else {
			addCall(mocks.route.EXPECT().Index(gomock.Any(), gomock.Any()).Return(objects.Routes, nil))
		}
		for _, fn := range routeOverlay {
			fn(tgtObjects)
		}

		// getSharedRules
		if len(wantSharedRulesKeys) != 0 {
			srFilters := []interface{}{}
			for _, k := range wantSharedRulesKeys {
				srFilters = append(srFilters, service.SharedRulesFilter{ZoneKey: zk, SharedRulesKey: k})
			}
			addCall(mocks.sharedRules.EXPECT().Index(srFilters...).Return(objects.SharedRules, nil))
		} else {
			addCall(mocks.sharedRules.EXPECT().Index(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(objects.SharedRules, nil))
		}
		for _, fn := range sharedRulesOverlay {
			fn(tgtObjects)
		}

		// getClusters
		if len(wantClusterKeys) != 0 {
			cfFilters := []interface{}{}
			for _, k := range wantClusterKeys {
				cfFilters = append(cfFilters, service.ClusterFilter{ZoneKey: zk, ClusterKey: k})
			}
			addCall(mocks.cluster.EXPECT().Index(cfFilters...).Return(objects.Clusters, nil))
		} else {
			addCall(mocks.cluster.EXPECT().Index(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(objects.Clusters, nil))
		}
		for _, fn := range clusterOverlay {
			fn(tgtObjects)
		}
	}

	gomock.InOrder(calls...)

	co, err := mocks.ObjectsWithOverrides("PK-1", overrides, nil)
	if errMsg == "" {
		if assert.Nil(t, err) {
			assert.DeepEqual(t, co.Proxy, tgtObjects.Proxy)
			assert.DeepEqual(t, co.Domains, tgtObjects.Domains)
			assert.DeepEqual(t, co.Routes, tgtObjects.Routes)
			assert.DeepEqual(t, co.SharedRules, tgtObjects.SharedRules)
		}
	} else {
		assert.ErrorContains(t, err, errMsg)
	}
}

func TestRemoteObjectsOverridesProxy(t *testing.T) {
	overlayTest(
		t,
		map[string]ObjectsUpdateFn{
			"PK-1": func(c *Objects) {
				c.Proxy.Name = "new-proxy-name"
			},
		},
		[]api.DomainKey{"D0", "D1"},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
	)
}

func TestRemoteObjectsOverridesProxyDeletion(t *testing.T) {
	overlayTest(
		t,
		map[string]ObjectsUpdateFn{
			"PK-1": func(c *Objects) {
				c.Proxy = api.Proxy{}
				c.Checksum = api.Checksum{}
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
	)
}

func TestRemoteObjectsOverridesDomain1(t *testing.T) {
	overlayTest(
		t,
		nil,
		[]api.DomainKey{"D0", "D1"},
		map[string]ObjectsUpdateFn{
			"D1": func(c *Objects) {
				for i, d := range c.Domains {
					if d.DomainKey == "D1" {
						d.Name = "updated.example.com"
						c.Domains[i] = d
						return
					}
				}
			},
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
	)
}

func TestRemoteObjectsOverridesRoute1(t *testing.T) {
	overlayTest(
		t,
		nil,
		nil,
		nil,
		map[string]ObjectsUpdateFn{
			"R1": func(c *Objects) {
				for i, r := range c.Routes {
					if r.RouteKey == "R1" {
						r.SharedRulesKey = "SRK-0"
						c.Routes[i] = r
					}
				}
			},
		},
		[]api.SharedRulesKey{"SRK-0", "SRK-2"},
		nil,
		[]api.ClusterKey{"C0", "C1"},
		nil,
		"",
	)
}

func TestRemoteObjectsOverridesSRK1(t *testing.T) {
	overlayTest(
		t,
		nil,
		nil,
		nil,
		nil,
		nil,
		map[string]ObjectsUpdateFn{
			"SRK-1": func(c *Objects) {
				for i, sr := range c.SharedRules {
					if sr.SharedRulesKey == "SRK-1" {
						sr.Default.Light = sr.Default.Light[0:2]
						c.SharedRules[i] = sr
					}
				}
			},
		},
		[]api.ClusterKey{"C0", "C1"},
		nil,
		"",
	)
}

func TestRemoteObjectsOverridesC3(t *testing.T) {
	overlayTest(
		t,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		map[string]ObjectsUpdateFn{
			"C3": func(c *Objects) {
				for i, cl := range c.Clusters {
					if cl.ClusterKey == "C3" {
						cl.Name = "asonetuhasoentuh"
						c.Clusters[i] = cl
					}
				}
			},
		},
		"",
	)
}

func TestRemoteObjectsOverridesCausesFailure(t *testing.T) {
	overlayTest(
		t,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		map[string]ObjectsUpdateFn{
			"C3": func(c *Objects) {
				rm := -1
				for i, cl := range c.Clusters {
					if cl.ClusterKey == "C3" {
						rm = i
					}
				}

				newCl := c.Clusters
				switch {
				case rm == -1:
					assert.Failed(t, "bad ConfigObjects mutation")
				case rm == len(c.Clusters)-1:
					newCl = c.Clusters[0:rm]
				default:
					newCl = append(
						c.Clusters[0:rm], c.Clusters[rm+1:len(c.Clusters)]...)
				}

				c.Clusters = newCl
			},
		},
		"unknown cluster C3",
	)
}
