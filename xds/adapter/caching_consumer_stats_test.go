package adapter

import (
	"fmt"
	"testing"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/googleapis/google/rpc"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api/service/stats/v2"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

var testEDSRequest = &envoyapi.DiscoveryRequest{
	TypeUrl: cache.EndpointType,
	Node: &core.Node{
		Id:      "node",
		Cluster: "proxy",
		Locality: &core.Locality{
			Zone: "zone",
		},
	},
}

var testCDSRequest = &envoyapi.DiscoveryRequest{
	TypeUrl: cache.ClusterType,
	Node: &core.Node{
		Id:      "node",
		Cluster: "proxy",
		Locality: &core.Locality{
			Zone: "zone",
		},
	},
}

func withCurrentTimeFrozen(f func(cs tbntime.ControlledSource)) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		defer func() {
			timeSource = tbntime.NewSource()
		}()

		timeSource = cs

		f(cs)
	})
}

func TestNewStreamState(t *testing.T) {
	assert.DeepEqual(t, newStreamState(&envoyapi.DiscoveryRequest{}), &streamState{})

	assert.DeepEqual(
		t,
		newStreamState(&envoyapi.DiscoveryRequest{TypeUrl: "type-url"}),
		&streamState{typeURL: "type-url"},
	)

	assert.DeepEqual(
		t,
		newStreamState(
			&envoyapi.DiscoveryRequest{
				TypeUrl: "type-url",
				Node: &core.Node{
					Id: "node-id",
				},
			},
		),
		&streamState{
			typeURL: "type-url",
			nodeID:  "node-id",
		},
	)

	assert.DeepEqual(
		t,
		newStreamState(
			&envoyapi.DiscoveryRequest{
				TypeUrl: "type-url",
				Node: &core.Node{
					Id:      "node-id",
					Cluster: "proxy",
				},
			},
		),
		&streamState{
			typeURL: "type-url",
			nodeID:  "node-id",
			proxy:   "proxy",
		},
	)

	assert.DeepEqual(
		t,
		newStreamState(
			&envoyapi.DiscoveryRequest{
				TypeUrl: "type-url",
				Node: &core.Node{
					Id:      "node-id",
					Cluster: "proxy",
					Locality: &core.Locality{
						Zone: "zone",
					},
				},
			},
		),
		&streamState{
			typeURL: "type-url",
			nodeID:  "node-id",
			proxy:   "proxy",
			zone:    "zone",
		},
	)
}

func TestStreamStateStart(t *testing.T) {
	// First request
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)

		state := newStreamState(testEDSRequest)
		state.start(mockStats)

		assert.Equal(t, state.requestStart, cs.Now())
		assert.True(t, state.responseComplete.IsZero())
	})

	// Subsequent requests
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		state := newStreamState(testEDSRequest)
		state.responseComplete = cs.Now()
		cs.Advance(time.Second)

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Timing(
			v2.ConfigInterval,
			time.Second,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)
		state.start(mockStats)

		assert.Equal(t, state.requestStart, cs.Now())

		assert.True(t, state.responseComplete.IsZero())
	})
}

func TestStreamStateComplete(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Timing(
			v2.ConfigLatency,
			time.Second,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)

		state := newStreamState(testEDSRequest)
		state.start(mockStats)

		cs.Advance(time.Second)

		state.complete(mockStats, "version", "nonce")

		assert.Equal(t, state.responseComplete, cs.Now())
		assert.Equal(t, state.responseVersion, "version")
		assert.Equal(t, state.responseNonce, "nonce")
	})

	// Missing request start (verifies version/nonce recorded so that stats may
	// recover on a new request)
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)

		state := newStreamState(testEDSRequest)
		state.complete(mockStats, "version", "nonce")

		assert.Equal(t, state.responseComplete, cs.Now())
		assert.Equal(t, state.responseVersion, "version")
		assert.Equal(t, state.responseNonce, "nonce")
	})
}

func TestStreamStateAck(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Count(
			v2.Config,
			1.0,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigState, v2.ConfigValid),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)

		state := newStreamState(testEDSRequest)
		state.ack(mockStats)

		// No changes:
		assert.DeepEqual(t, state, newStreamState(testEDSRequest))
	})
}

func TestStreamStateNack(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Count(
			v2.Config,
			1.0,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigState, v2.ConfigInvalid),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)

		state := newStreamState(testEDSRequest)
		state.nack(mockStats)

		// No changes:
		assert.DeepEqual(t, state, newStreamState(testEDSRequest))
	})
}

func TestNewStreamStateMap(t *testing.T) {
	m := newStreamStateMap()
	assert.NonNil(t, m.mutex)
	assert.NonNil(t, m.state)
}

func TestStreamStateMapGet(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)

	m := newStreamStateMap()

	// 1/eds
	state1a, created1a := m.get(1, testEDSRequest)
	assert.DeepEqual(t, state1a, newStreamState(testEDSRequest))
	assert.True(t, created1a)

	// 2/eds
	state2, created2 := m.get(2, testEDSRequest)
	assert.DeepEqual(t, state2, newStreamState(testEDSRequest))
	assert.True(t, created2)
	assert.NotSameInstance(t, state1a, state2)

	// 1/eds (again)
	state1a.start(mockStats)
	state1b, created1b := m.get(1, testEDSRequest)
	assert.SameInstance(t, state1b, state1a)
	assert.False(t, created1b)

	// 1/cds
	state1c, created1c := m.get(1, testCDSRequest)
	assert.DeepEqual(t, state1c, newStreamState(testCDSRequest))
	assert.True(t, created1c)
	assert.NotSameInstance(t, state1c, state1a)
}

func TestStreamStateMapRemove(t *testing.T) {
	m := newStreamStateMap()

	// 1/eds
	state1a, created1a := m.get(1, testEDSRequest)
	assert.DeepEqual(t, state1a, newStreamState(testEDSRequest))
	assert.True(t, created1a)

	// 2/eds
	state2, created2 := m.get(2, testEDSRequest)
	assert.DeepEqual(t, state2, newStreamState(testEDSRequest))
	assert.True(t, created2)
	assert.NotSameInstance(t, state1a, state2)

	m.remove(1)
	_, created := m.get(1, testEDSRequest)
	assert.True(t, created)

	_, created = m.get(2, testEDSRequest)
	assert.False(t, created)
}

func TestNewCachingConsumerStats(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockConsumer := newMockCachingConsumer(ctrl)
	mockStats := stats.NewMockStats(ctrl)

	ccs := newCachingConsumerStats(mockConsumer, mockStats)
	ccsImpl := ccs.(*cachingConsumerStats)
	assert.SameInstance(t, ccsImpl.cachingConsumer, mockConsumer)
	assert.SameInstance(t, ccsImpl.stats, mockStats)
	assert.NonNil(t, ccsImpl.streamState)
}

func TestCachingConsumerStatsStartFirstRequest(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)

		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), testEDSRequest)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)
		ccs.OnStreamRequest(1, testEDSRequest)
		assert.ChannelEmpty(t, ch)

		streamState, created := ccs.(*cachingConsumerStats).streamState.get(1, testEDSRequest)
		assert.False(t, created)
		assert.Equal(t, streamState.requestStart, cs.Now())
	})
}

func TestCachingConsumerStatsStartFirstRequestWithUnexpectedNonce(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)

		req := *testEDSRequest
		req.ResponseNonce = "nope"

		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), &req)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)
		ccs.OnStreamRequest(1, &req)
		msg := <-ch
		assert.Equal(
			t,
			msg.Message,
			fmt.Sprintf(
				"[error] Stream 1, %s: new stream with unexpected nonce %q\n",
				cache.EndpointType,
				"nope",
			),
		)

		streamState, created := ccs.(*cachingConsumerStats).streamState.get(1, &req)
		assert.False(t, created)
		assert.Equal(t, streamState.requestStart, cs.Now())
	})
}

func TestCachingConsumerStatsStartRequestWithWrongNonce(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		req := *testEDSRequest

		mockStats := stats.NewMockStats(ctrl)
		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), &req)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)

		streamState, _ := ccs.(*cachingConsumerStats).streamState.get(1, &req)
		streamState.responseNonce = "nonce"

		req.ResponseNonce = "wrong"

		ccs.OnStreamRequest(1, &req)
		msg := <-ch
		assert.Equal(
			t,
			msg.Message,
			fmt.Sprintf(
				"[error] Stream 1, %s: got nonce %q, expected %q\n",
				cache.EndpointType,
				"wrong",
				"nonce",
			),
		)

		streamState, _ = ccs.(*cachingConsumerStats).streamState.get(1, &req)
		assert.True(t, streamState.requestStart.IsZero())
	})
}

func TestCachingConsumerStatsStartRequestDetectsAck(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		req := *testEDSRequest
		req.ResponseNonce = "nonce"
		req.VersionInfo = "version"

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Count(
			v2.Config,
			1.0,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigState, v2.ConfigValid),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)
		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), &req)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)

		streamState, _ := ccs.(*cachingConsumerStats).streamState.get(1, &req)
		streamState.responseNonce = "nonce"
		streamState.responseVersion = "version"

		ccs.OnStreamRequest(1, &req)
		msg := <-ch
		assert.Equal(
			t,
			msg.Message,
			fmt.Sprintf(
				"[info] Stream 1, %s: ack response version %q\n",
				cache.EndpointType,
				"version",
			),
		)

		streamState, _ = ccs.(*cachingConsumerStats).streamState.get(1, &req)
		assert.Equal(t, streamState.requestStart, cs.Now())
	})
}

func TestCachingConsumerStatsStartRequestDetectsNack(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		req := *testEDSRequest
		req.ResponseNonce = "nonce"
		req.VersionInfo = "version"
		req.ErrorDetail = &rpc.Status{
			Code:    int32(rpc.INTERNAL),
			Message: "oops",
		}

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Count(
			v2.Config,
			1.0,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigState, v2.ConfigInvalid),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)
		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), &req)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)

		streamState, _ := ccs.(*cachingConsumerStats).streamState.get(1, &req)
		streamState.responseNonce = "nonce"
		streamState.responseVersion = "newer-version"

		ccs.OnStreamRequest(1, &req)
		msg := <-ch
		assert.Equal(
			t,
			msg.Message,
			fmt.Sprintf(
				"[error] Stream 1, %s: NACK response version %q (last ack is %q)\n",
				cache.EndpointType,
				"newer-version",
				"version",
			),
		)

		msg = <-ch
		assert.Equal(
			t,
			msg.Message,
			fmt.Sprintf(
				"[error] Stream 1, %s: Error Code INTERNAL, Message %q\n",
				cache.EndpointType,
				"oops",
			),
		)

		streamState, _ = ccs.(*cachingConsumerStats).streamState.get(1, &req)
		assert.Equal(t, streamState.requestStart, cs.Now())
	})
}

func TestCachingConsumerStatsCompleteRequest(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Timing(
			v2.ConfigLatency,
			100*time.Millisecond,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)

		req := *testEDSRequest

		resp := &envoyapi.DiscoveryResponse{
			VersionInfo: "version",
			Nonce:       "nonce",
		}

		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnStreamRequest(int64(1), &req)
		mockConsumer.EXPECT().OnStreamResponse(int64(1), &req, resp)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)
		ccs.OnStreamRequest(1, &req)

		cs.Advance(100 * time.Millisecond)

		ccs.OnStreamResponse(1, &req, resp)

		streamState, created := ccs.(*cachingConsumerStats).streamState.get(1, &req)
		assert.False(t, created)
		assert.Equal(t, streamState.responseComplete, cs.Now())
		assert.Equal(t, streamState.responseVersion, "version")
		assert.Equal(t, streamState.responseNonce, "nonce")
	})
}

func TestCachingConsumerStatsOnStreamOpen(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)
	mockConsumer := newMockCachingConsumer(ctrl)
	mockConsumer.EXPECT().OnStreamOpen(int64(1), "type-url")

	ccs := newCachingConsumerStats(mockConsumer, mockStats)

	ccsImpl := ccs.(*cachingConsumerStats)
	ccsImpl.startRequest(1, testEDSRequest)
	ccsImpl.startRequest(2, testCDSRequest)

	ccs.OnStreamOpen(1, "type-url")
	assert.Nil(t, ccsImpl.streamState.state[1])
	assert.NotDeepEqual(t, ccsImpl.streamState.state[2], map[string]*streamState{})
}

func TestCachingConsumerStatsOnStreamClosed(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)
	mockConsumer := newMockCachingConsumer(ctrl)
	mockConsumer.EXPECT().OnStreamClosed(int64(1))

	ccs := newCachingConsumerStats(mockConsumer, mockStats)

	ccsImpl := ccs.(*cachingConsumerStats)
	ccsImpl.startRequest(1, testEDSRequest)
	ccsImpl.startRequest(2, testCDSRequest)

	ccs.OnStreamClosed(1)
	assert.Nil(t, ccsImpl.streamState.state[1])
	assert.NotDeepEqual(t, ccsImpl.streamState.state[2], map[string]*streamState{})
}

func TestCachingConsumerStatsOnFetchRequest(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)

		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnFetchRequest(testEDSRequest)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)
		ccs.OnFetchRequest(testEDSRequest)

		streamState, created :=
			ccs.(*cachingConsumerStats).streamState.get(fetchStream, testEDSRequest)
		assert.False(t, created)
		assert.Equal(t, streamState.requestStart, cs.Now())
	})
}

func TestCachingConsumerStatsOnFetchResponse(t *testing.T) {
	withCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		ctrl := gomock.NewController(assert.Tracing(t))
		defer ctrl.Finish()

		mockStats := stats.NewMockStats(ctrl)
		mockStats.EXPECT().Timing(
			v2.ConfigLatency,
			100*time.Millisecond,
			stats.NewKVTag(stats.NodeTag, "node"),
			stats.NewKVTag(stats.ProxyTag, "proxy"),
			stats.NewKVTag(stats.ZoneTag, "zone"),
			stats.NewKVTag(v2.ConfigType, cache.EndpointType),
		)

		req := *testEDSRequest

		resp := &envoyapi.DiscoveryResponse{
			VersionInfo: "version",
			Nonce:       "nonce",
		}

		mockConsumer := newMockCachingConsumer(ctrl)
		mockConsumer.EXPECT().OnFetchRequest(&req)
		mockConsumer.EXPECT().OnFetchResponse(&req, resp)

		ccs := newCachingConsumerStats(mockConsumer, mockStats)
		ccs.OnFetchRequest(&req)

		cs.Advance(100 * time.Millisecond)

		ccs.OnFetchResponse(&req, resp)

		streamState, created := ccs.(*cachingConsumerStats).streamState.get(fetchStream, &req)
		assert.False(t, created)
		assert.Equal(t, streamState.responseComplete, cs.Now())
		assert.Equal(t, streamState.responseVersion, "version")
		assert.Equal(t, streamState.responseNonce, "nonce")
	})
}
