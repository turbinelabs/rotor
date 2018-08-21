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
	"flag"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/matcher"
)

type cachingConsumerMocks struct {
	ctrl       *gomock.Controller
	cache      *mockSnapshotCache
	registrar  *poller.MockRegistrar
	adapt      snapshotAdapter
	streamRefs *mockStreamRefsIface
	consumer   registeringCachingConsumer
	req        *v2.DiscoveryRequest
	resp       *v2.DiscoveryResponse
	pRef       service.ProxyRef
	api        service.All
	proxy      api.Proxy
	mapKey     string
	snapshot   cache.Snapshot
}

func newCachingConsumerMocks(t *testing.T, adapt snapshotAdapter) cachingConsumerMocks {
	ctrl := gomock.NewController(assert.Tracing(t))
	mockCache := newMockSnapshotCache(ctrl)
	registrar := poller.NewMockRegistrar(ctrl)
	streamRefs := newMockStreamRefsIface(ctrl)

	req := &v2.DiscoveryRequest{
		Node: &core.Node{
			Id:      "the-id",
			Cluster: "that-proxy",
			Locality: &core.Locality{
				Zone: "that-zone",
			},
		},
		ResourceNames: []string{"one-response-name", "another-response-name"},
		ResponseNonce: "the-nonce",
		TypeUrl:       "the-type-url",
		VersionInfo:   "the-version-info",
	}

	return cachingConsumerMocks{
		ctrl:       ctrl,
		cache:      mockCache,
		registrar:  registrar,
		adapt:      adapt,
		streamRefs: streamRefs,
		consumer: registeringCachingConsumer{
			cache:      mockCache,
			registrar:  registrar,
			adapt:      adapt,
			streamRefs: streamRefs,
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

func TestCachingConsumerOnRequest(t *testing.T) {
	fs := tbnflag.Wrap(&flag.FlagSet{})
	console.Init(fs)
	fs.Unwrap().Parse([]string{"--console.level=debug"})
	defer fs.Unwrap().Parse([]string{"--console.level=error"})

	ch, cleanup := console.ConsumeConsoleLogs(3)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.consumer.getObjects = func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
		return poller.MkFixtureObjects(), nil
	}

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Register(mocks.pRef, capture).Return(nil)
	mocks.consumer.onRequest(23, mocks.req)

	mocks.cache.EXPECT().SetSnapshot(mocks.mapKey, mocks.snapshot).Return(nil)
	f := capture.V.(func(service.All, api.Proxy))
	f(mocks.api, mocks.proxy)

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[debug] "+`
-----------
    STREAM: 23
  RECEIVED: the-type-url
      NODE: the-id
   CLUSTER: that-proxy
  LOCALITY: zone:"that-zone"`+" "+`
     NAMES: one-response-name, another-response-name
     NONCE: the-nonce
   VERSION: the-version-info
`,
	)

	msg = <-ch
	assert.Equal(
		t,
		msg.Message,
		"[debug] First registration of {\"proxy_name\":\"that-proxy\",\"zone_name\":\"that-zone\"}\n",
	)

	msg = <-ch
	assert.StringContains(
		t,
		msg.Message,
		"[debug] Consuming the-zone main-test-proxy",
	)
}

func TestCachingConsumerOnRequestLoopkupErr(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(1)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()
	mocks.consumer.getObjects = func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
		return nil, errors.New("boom")
	}

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Register(mocks.pRef, capture).Return(nil)
	mocks.consumer.onRequest(23, mocks.req)

	f := capture.V.(func(service.All, api.Proxy))
	f(mocks.api, mocks.proxy)

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[error] Error getting initial objects for node({\"proxy_name\":\"that-proxy\",\"zone_name\":\"that-zone\"}): boom\n",
	)
}

func TestCachingConsumerOnRequestSetSnapshotErr(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(1)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.consumer.getObjects = func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
		return poller.MkFixtureObjects(), nil
	}

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Register(mocks.pRef, capture).Return(nil)
	mocks.consumer.onRequest(23, mocks.req)

	mocks.cache.EXPECT().SetSnapshot(mocks.mapKey, mocks.snapshot).Return(errors.New("boom"))
	f := capture.V.(func(service.All, api.Proxy))
	f(mocks.api, mocks.proxy)

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[error] Error consuming objects for node({\"proxy_name\":\"that-proxy\",\"zone_name\":\"that-zone\"}): boom\n",
	)
}

func TestCachingConsumerOnRequestRegErr(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Register(mocks.pRef, gomock.Any()).Return(errors.New("boom"))
	mocks.consumer.onRequest(23, mocks.req)

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[error] Error registering node({\"proxy_name\":\"that-proxy\",\"zone_name\":\"that-zone\"}): boom\n",
	)
}

func TestCachingConsumerOnResponse(t *testing.T) {
	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	capture := matcher.CaptureAny()
	mocks.registrar.EXPECT().Deregister(mocks.pRef, capture).Return(nil)
	mocks.consumer.onResponse(23, mocks.req, mocks.resp)

	mocks.cache.EXPECT().ClearSnapshot(mocks.pRef.MapKey())
	f := capture.V.(func())
	f()
}

func TestCachingConsumerOnResponseDeregErr(t *testing.T) {
	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Deregister(mocks.pRef, gomock.Any()).Return(errors.New("boom"))
	mocks.consumer.onResponse(23, mocks.req, mocks.resp)
}

func TestCachingConsumerOnStreamOpen(t *testing.T) {
	fs := tbnflag.Wrap(&flag.FlagSet{})
	console.Init(fs)
	fs.Unwrap().Parse([]string{"--console.level=debug"})
	defer fs.Unwrap().Parse([]string{"--console.level=error"})

	ch, cleanup := console.ConsumeConsoleLogs(1)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	mocks.consumer.OnStreamOpen(23, "the-type")

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[debug] stream open:  23 the-type\n",
	)
}

func TestCachingConsumerOnStreamClosed(t *testing.T) {
	fs := tbnflag.Wrap(&flag.FlagSet{})
	console.Init(fs)
	fs.Unwrap().Parse([]string{"--console.level=debug"})
	defer fs.Unwrap().Parse([]string{"--console.level=error"})

	ch, cleanup := console.ConsumeConsoleLogs(3)
	defer cleanup()

	mocks := newCachingConsumerMocks(t, nil)
	defer mocks.ctrl.Finish()

	ref1 := service.NewMockProxyRef(mocks.ctrl)
	ref2 := service.NewMockProxyRef(mocks.ctrl)

	capture := matcher.CaptureAny()

	gomock.InOrder(
		mocks.streamRefs.EXPECT().RemoveAll(int64(23)).Return([]service.ProxyRef{
			ref1,
			ref2,
			ref1,
		}),
		ref1.EXPECT().MapKey().Return("key-1"),
		mocks.registrar.EXPECT().Deregister(ref1, gomock.Any()).Return(nil),
		ref2.EXPECT().MapKey().Return("key-2"),
		mocks.registrar.EXPECT().Deregister(ref2, gomock.Any()).Return(nil),
		ref1.EXPECT().MapKey().Return("key-1"),
		mocks.registrar.EXPECT().Deregister(ref1, capture).Return(errors.New("boom")),
		mocks.cache.EXPECT().ClearSnapshot("key-1"),
	)

	mocks.consumer.OnStreamClosed(23)

	f := capture.V.(func())
	f()

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[error] Error deregistering node(key-1): boom\n",
	)

	msg = <-ch
	assert.Equal(
		t,
		msg.Message,
		"[debug] stream closed:  23\n",
	)

	msg = <-ch
	assert.Equal(
		t,
		msg.Message,
		"[debug] Last deregistration of key-1\n",
	)
}

func TestCachingConsumerOnFetchRequest(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Register(mocks.pRef, gomock.Any()).Return(nil)

	mocks.consumer.OnFetchRequest(mocks.req)
}

func TestCachingConsumerOnFetchResponse(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	mocks.registrar.EXPECT().Deregister(mocks.pRef, gomock.Any()).Return(nil)

	mocks.consumer.OnFetchResponse(mocks.req, mocks.resp)
}

func TestCachingConsumerOnStreamRequest(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	gomock.InOrder(
		mocks.streamRefs.EXPECT().Add(int64(23), mocks.pRef),
		mocks.registrar.EXPECT().Register(mocks.pRef, gomock.Any()).Return(nil),
	)

	mocks.consumer.OnStreamRequest(23, mocks.req)
}

func TestCachingConsumerOnStreamResponse(t *testing.T) {
	mocks := newCachingConsumerMocks(t, successSnapshotAdapter)
	defer mocks.ctrl.Finish()

	gomock.InOrder(
		mocks.registrar.EXPECT().Deregister(mocks.pRef, gomock.Any()).Return(nil),
		mocks.streamRefs.EXPECT().Remove(int64(23), mocks.pRef),
	)

	mocks.consumer.OnStreamResponse(23, mocks.req, mocks.resp)
}

func TestStreamRefsAddRemove(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	refs := newStreamRefs().(*streamRefs)
	assert.Equal(t, len(refs.refs), 0)

	ref := service.NewMockProxyRef(ctrl)
	ref.EXPECT().MapKey().Return("that-key").AnyTimes()

	ref2 := service.NewMockProxyRef(ctrl)
	ref2.EXPECT().MapKey().Return("that-other-key").AnyTimes()

	refs.Add(64, ref)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 1)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 1})

	refs.Add(64, ref)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 1)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})

	refs.Add(64, ref2)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Add(23, ref)
	assert.Equal(t, len(refs.refs), 2)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Remove(66, ref)
	refs.Remove(23, ref2)
	assert.Equal(t, len(refs.refs), 2)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Remove(64, ref)
	assert.Equal(t, len(refs.refs), 2)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 1})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Remove(64, ref)
	assert.Equal(t, len(refs.refs), 2)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
	assert.Equal(t, len(refs.refs[64]), 1)
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Remove(64, ref2)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})

	refs.Remove(23, ref)
	assert.Equal(t, len(refs.refs), 0)
}

func TestStreamRefsRemoveAll(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	refs := newStreamRefs().(*streamRefs)
	assert.Equal(t, len(refs.refs), 0)

	ref := service.NewMockProxyRef(ctrl)
	ref.EXPECT().MapKey().Return("that-key").AnyTimes()

	ref2 := service.NewMockProxyRef(ctrl)
	ref2.EXPECT().MapKey().Return("that-other-key").AnyTimes()

	refs.Add(64, ref)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 1)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 1})

	refs.Add(64, ref)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 1)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})

	refs.Add(64, ref2)
	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	refs.Add(23, ref)
	assert.Equal(t, len(refs.refs), 2)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
	assert.Equal(t, len(refs.refs[64]), 2)
	assert.DeepEqual(t, refs.refs[64]["that-key"], &streamRefEntry{ref, 2})
	assert.DeepEqual(t, refs.refs[64]["that-other-key"], &streamRefEntry{ref, 1})

	want := []service.ProxyRef{
		ref,
		ref,
		ref2,
	}

	assert.Nil(t, refs.RemoveAll(66))

	got := refs.RemoveAll(64)
	assert.HasSameElements(t, got, want)

	assert.Equal(t, len(refs.refs), 1)
	assert.Equal(t, len(refs.refs[23]), 1)
	assert.DeepEqual(t, refs.refs[23]["that-key"], &streamRefEntry{ref, 1})
}
