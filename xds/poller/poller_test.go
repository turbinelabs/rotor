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
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

type pollerTestCase struct {
	proxyRefGetErr   bool
	remoteObjectsErr bool
	consumeErr       bool
	wantErr          error
}

var (
	errPollerTestRemoteObjects = errors.New("Remote.Objects")
	errPollerTestConsume       = errors.New("Consumer.Consume")
	errPollerTestProxyRefGet   = errors.New("ProxyRef.Get")
)

func (tc pollerTestCase) run(t testing.TB) {
	ctrl := gomock.NewController(assert.Tracing(t))
	calls := make([]*gomock.Call, 0)
	addCalls := func(c ...*gomock.Call) {
		calls = append(calls, c...)
	}

	remote := NewMockRemote(ctrl)
	consumer := NewMockConsumer(ctrl)
	registrar := NewMockRegistrar(ctrl)
	proxyRef := service.NewMockProxyRef(ctrl)
	zoneRef := service.NewMockZoneRef(ctrl)
	svc := service.NewMockAll(ctrl)
	mockStats := stats.NewMockStats(ctrl)

	objects := &Objects{}
	proxy := api.Proxy{ProxyKey: "PK-1", Name: "that-proxy"}

	defer func() {
		gomock.InOrder(calls...)
		p := poller{
			svc:       svc,
			remote:    remote,
			consumer:  consumer,
			registrar: registrar,
			stats:     mockStats,
		}
		assert.Equal(t, p.Poll(), tc.wantErr)
		ctrl.Finish()
	}()

	mkStatCalls := func(result string) []*gomock.Call {
		return []*gomock.Call{
			proxyRef.EXPECT().ZoneRef().Return(zoneRef),
			zoneRef.EXPECT().Name().Return("that-zone"),
			mockStats.EXPECT().Count(
				"poll",
				1.0,
				stats.NewKVTag("result", result),
				stats.NewKVTag(stats.ProxyTag, "that-proxy"),
				stats.NewKVTag(stats.ZoneTag, "that-zone"),
			),
		}
	}

	addCalls(
		registrar.EXPECT().Refs().Return([]service.ProxyRef{proxyRef}),
		proxyRef.EXPECT().Name().Return("that-proxy"),
	)

	if tc.proxyRefGetErr {
		addCalls(proxyRef.EXPECT().Get(svc).Return(api.Proxy{}, errPollerTestProxyRefGet))
		addCalls(mkStatCalls("error")...)
		return
	}

	addCalls(proxyRef.EXPECT().Get(svc).Return(proxy, nil))

	if tc.remoteObjectsErr {
		addCalls(remote.EXPECT().Objects(proxy.ProxyKey).Return(nil, errPollerTestRemoteObjects))
		addCalls(mkStatCalls("error")...)
		return
	}

	addCalls(remote.EXPECT().Objects(proxy.ProxyKey).Return(objects, nil))

	if tc.consumeErr {
		addCalls(consumer.EXPECT().Consume(objects).Return(errPollerTestConsume))
		addCalls(mkStatCalls("error")...)
		return
	}

	addCalls(consumer.EXPECT().Consume(objects).Return(nil))
	addCalls(mkStatCalls("success")...)
}

func TestPollerProxyRefGetErr(t *testing.T) {
	pollerTestCase{
		proxyRefGetErr: true,
		wantErr:        errPollerTestProxyRefGet,
	}.run(t)
}

func TestPollerRemoteObjectsErr(t *testing.T) {
	pollerTestCase{
		remoteObjectsErr: true,
		wantErr:          errPollerTestRemoteObjects,
	}.run(t)
}

func TestPollerConsumeErr(t *testing.T) {
	pollerTestCase{
		consumeErr: true,
		wantErr:    errPollerTestConsume,
	}.run(t)
}

func TestPollerSuccess(t *testing.T) {
	pollerTestCase{}.run(t)
}

func TestPollerPollLoop(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	remote := NewMockRemote(ctrl)
	consumer := NewMockConsumer(ctrl)
	registrar := NewMockRegistrar(ctrl)
	proxyRef := service.NewMockProxyRef(ctrl)
	zoneRef := service.NewMockZoneRef(ctrl)
	svc := service.NewMockAll(ctrl)
	mockStats := stats.NewMockStats(ctrl)

	objects := &Objects{}
	proxy := api.Proxy{ProxyKey: "PK-1", Name: "that-proxy"}

	gomock.InOrder(
		registrar.EXPECT().Refs().Return([]service.ProxyRef{proxyRef}),
		proxyRef.EXPECT().Name().Return("that-proxy"),
		proxyRef.EXPECT().Get(svc).Return(proxy, nil),
		remote.EXPECT().Objects(proxy.ProxyKey).Return(objects, nil),
		consumer.EXPECT().Consume(objects).Return(nil),
		proxyRef.EXPECT().ZoneRef().Return(zoneRef),
		zoneRef.EXPECT().Name().Return("that-zone"),
		mockStats.EXPECT().Count(
			"poll",
			1.0,
			stats.NewKVTag("result", "success"),
			stats.NewKVTag(stats.ProxyTag, "that-proxy"),
			stats.NewKVTag(stats.ZoneTag, "that-zone"),
		),

		registrar.EXPECT().Refs().Return([]service.ProxyRef{proxyRef}),
		proxyRef.EXPECT().Name().Return("that-proxy"),
		proxyRef.EXPECT().Get(svc).Return(proxy, nil),
		remote.EXPECT().
			Objects(proxy.ProxyKey).
			Return(nil, errPollerTestRemoteObjects),
		proxyRef.EXPECT().ZoneRef().Return(zoneRef),
		zoneRef.EXPECT().Name().Return("that-zone"),
		mockStats.EXPECT().Count(
			"poll",
			1.0,
			stats.NewKVTag("result", "error"),
			stats.NewKVTag(stats.ProxyTag, "that-proxy"),
			stats.NewKVTag(stats.ZoneTag, "that-zone"),
		),
	)

	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		p := poller{
			svc:          svc,
			remote:       remote,
			consumer:     consumer,
			registrar:    registrar,
			pollInterval: time.Second,
			stats:        mockStats,
			time:         cs,
			quitCh:       make(chan struct{}),
		}

		goroutineExit := &sync.WaitGroup{}
		goroutineExit.Add(1)

		go func() {
			defer goroutineExit.Done()
			p.PollLoop()
		}()

		for cs.TriggerAllTimers() == 0 {
			time.Sleep(10 * time.Millisecond)
		}

		assert.Nil(t, p.Close())
		goroutineExit.Wait()
	})
}

func TestPollerDoubleClose(t *testing.T) {
	p := poller{
		quitCh: make(chan struct{}),
	}
	assert.Nil(t, p.Close())
	assert.ErrorContains(t, p.Close(), "already closed")
}
