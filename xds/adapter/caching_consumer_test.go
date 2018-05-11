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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/matcher"
)

type cachingConsumerMocks struct {
	ctrl      *gomock.Controller
	cache     *mockSnapshotCache
	registrar *poller.MockRegistrar
	adapt     snapshotAdapter
	consumer  registeringCachingConsumer
	req       *v2.DiscoveryRequest
	resp      *v2.DiscoveryResponse
	pRef      service.ProxyRef
	api       service.All
	proxy     api.Proxy
	mapKey    string
	snapshot  cache.Snapshot
}

func newCachingConsumerMocks(t *testing.T, adapt snapshotAdapter) cachingConsumerMocks {
	ctrl := gomock.NewController(assert.Tracing(t))
	mockCache := newMockSnapshotCache(ctrl)
	registrar := poller.NewMockRegistrar(ctrl)

	req := &v2.DiscoveryRequest{
		Node: &core.Node{
			Cluster: "that-proxy",
			Locality: &core.Locality{
				Zone: "that-zone",
			},
		},
	}

	return cachingConsumerMocks{
		ctrl:      ctrl,
		cache:     mockCache,
		registrar: registrar,
		adapt:     adapt,
		consumer: registeringCachingConsumer{
			cache:     mockCache,
			registrar: registrar,
			adapt:     adapt,
		},
		req:      req,
		resp:     &v2.DiscoveryResponse{},
		pRef:     proxyRefFromNode(req.Node),
		api:      service.NewMockAll(ctrl),
		proxy:    poller.MkFixtureObjects().Proxy,
		mapKey:   `{"proxy_name":"main-test-proxy","zone_name":"the-zone"}`,
		snapshot: cache.NewSnapshot(poller.FixtureHash, nil, nil, nil, nil),
	}
}

var (
	successSnapshotAdapter = func(objects *poller.Objects) (cache.Snapshot, error) {
		return cache.NewSnapshot(objects.TerribleHash(), nil, nil, nil, nil), nil
	}

	errSnapshotAdapter = func(objects *poller.Objects) (cache.Snapshot, error) {
		return cache.Snapshot{}, errors.New(objects.TerribleHash())
	}
)

func TestCachingConsumerConsume(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.cache.EXPECT().SetSnapshot(mocks.mapKey, mocks.snapshot).Return(nil)

	err := mocks.consumer.Consume(poller.MkFixtureObjects())
	assert.Nil(t, err)
}

func TestCachingConsumerConsumeAdaptErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, errSnapshotAdapter)
	defer mocks.ctrl.Finish()

	err := mocks.consumer.Consume(poller.MkFixtureObjects())
	assert.Equal(t, err.Error(), poller.FixtureHash)
}

func TestCachingConsumerConsumeSetSnapshotErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.cache.EXPECT().SetSnapshot(mocks.mapKey, mocks.snapshot).Return(errors.New("boom"))

	err := mocks.consumer.Consume(poller.MkFixtureObjects())
	assert.Equal(t, err.Error(), "boom")
}

func TestCachingConsumerOnFetchRequest(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.consumer.getObjects = func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
		return poller.MkFixtureObjects(), nil
	}

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Register(mocks.pRef, capture).Return(nil)
	mocks.consumer.OnFetchRequest(mocks.req)

	mocks.cache.EXPECT().SetSnapshot(mocks.mapKey, mocks.snapshot).Return(nil)
	f := capture.V.(func(service.All, api.Proxy))
	f(mocks.api, mocks.proxy)
}

func TestCachingConsumerOnFetchRequestLoopkupErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()
	mocks.consumer.getObjects = func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
		return nil, errors.New("boom")
	}

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Register(mocks.pRef, capture).Return(nil)
	mocks.consumer.OnFetchRequest(mocks.req)

	f := capture.V.(func(service.All, api.Proxy))
	f(mocks.api, mocks.proxy)
}

func TestCachingConsumerOnFetchRequestRegErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Register(mocks.pRef, gomock.Any()).Return(errors.New("boom"))
	mocks.consumer.OnFetchRequest(mocks.req)
}

func TestCachingConsumerOnFetchResponse(t *testing.T) {
	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Deregister(mocks.pRef, capture).Return(nil)
	mocks.consumer.OnFetchResponse(mocks.req, mocks.resp)

	mocks.cache.EXPECT().ClearSnapshot(mocks.pRef.MapKey())
	f := capture.V.(func())
	f()
}

func TestCachingConsumerOnFetchResponseDeregErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Deregister(mocks.pRef, gomock.Any()).Return(errors.New("boom"))
	mocks.consumer.OnFetchResponse(mocks.req, mocks.resp)
}
