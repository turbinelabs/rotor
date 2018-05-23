/*
Copyright 2018 Turbine Labs, Inc.

nLicensed under the Apache License, Version 2.0 (the "License");
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
	"sync"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/googleapis/google/rpc"
	"github.com/turbinelabs/api/service/stats/v2"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/stats"
)

const fetchStream = -1

var timeSource = tbntime.NewSource()

// cachingConsumerStats is a wrapper for an underlying cachingConsumer that records
// stats about xDS requests.
type cachingConsumerStats struct {
	cachingConsumer

	stats       stats.Stats
	streamState *streamStateMap
}

// newCachingConsumerStats wraps the given cachingConsumer and reports stats on xDS
// requests to the tiven Stats.
func newCachingConsumerStats(
	underlying cachingConsumer,
	stats stats.Stats,
) cachingConsumer {
	return &cachingConsumerStats{
		cachingConsumer: underlying,
		stats:           stats,
		streamState:     newStreamStateMap(),
	}
}

func (c *cachingConsumerStats) startRequest(streamID int64, req *envoyapi.DiscoveryRequest) {
	streamState, created := c.streamState.get(streamID, req)
	if !created && req.GetResponseNonce() != streamState.responseNonce && streamID != fetchStream {
		// Wrong nonce. During streaming, go-control-plane won't actually respond in
		// this case (having invoked OnStreamRequest before checking). The Fetch path
		// doesn't check the nonce at all.
		console.Error().Printf(
			"Stream %d, %s: got nonce %q, expected %q",
			streamID,
			req.GetTypeUrl(),
			req.GetResponseNonce(),
			streamState.responseNonce,
		)
		return
	}

	// No matter what else we do, we've started a new request.
	streamState.start(c.stats)

	if created {
		// First request on this stream
		if req.GetResponseNonce() != "" {
			// Unexpected nonce value.
			console.Error().Printf(
				"Stream %d, %s: new stream with unexpected nonce %q",
				streamID,
				req.GetTypeUrl(),
				req.GetResponseNonce(),
			)
		}
		return
	}

	if streamState.responseVersion == req.GetVersionInfo() {
		// ack: correct nonce and version.
		console.Info().Printf(
			"Stream %d, %s: ack response version %q",
			streamID,
			req.GetTypeUrl(),
			streamState.responseVersion,
		)
		streamState.ack(c.stats)
		return
	}

	// nack: Correct nonce but wrong version.
	console.Error().Printf(
		"Stream %d, %s: NACK response version %q (last ack is %q)",
		streamID,
		req.GetTypeUrl(),
		streamState.responseVersion,
		req.GetVersionInfo(),
	)
	if detail := req.GetErrorDetail(); detail != nil {
		console.Error().Printf(
			"Stream %d, %s: Error Code %s, Message %q",
			streamID,
			req.GetTypeUrl(),
			rpc.Code(detail.GetCode()).String(),
			detail.GetMessage(),
		)
	}
	streamState.nack(c.stats)
}

func (c *cachingConsumerStats) completeResponse(
	streamID int64,
	req *envoyapi.DiscoveryRequest,
	resp *envoyapi.DiscoveryResponse,
) {
	streamState, _ := c.streamState.get(streamID, req)
	streamState.complete(c.stats, resp.GetVersionInfo(), resp.GetNonce())
}

// OnStreamOpen implements
// go-control-plane/pkg/server/Callbacks.OnStreamOpen
func (c *cachingConsumerStats) OnStreamOpen(streamID int64, typeURL string) {
	defer c.streamState.remove(streamID)
	c.cachingConsumer.OnStreamOpen(streamID, typeURL)
}

// OnStreamClosed implements
// go-control-plane/pkg/server/Callbacks.OnStreamClosed
func (c *cachingConsumerStats) OnStreamClosed(streamID int64) {
	defer c.streamState.remove(streamID)
	c.cachingConsumer.OnStreamClosed(streamID)
}

// OnStreamRequest implements
// go-control-plane/pkg/server/Callbacks.OnStreamRequest
func (c *cachingConsumerStats) OnStreamRequest(streamID int64, req *envoyapi.DiscoveryRequest) {
	defer c.startRequest(streamID, req)
	c.cachingConsumer.OnStreamRequest(streamID, req)
}

// OnStreamResponse implements
// go-control-plane/pkg/server/Callbacks.OnStreamResponse
func (c cachingConsumerStats) OnStreamResponse(
	streamID int64,
	req *envoyapi.DiscoveryRequest,
	resp *envoyapi.DiscoveryResponse,
) {
	defer c.completeResponse(streamID, req, resp)
	c.cachingConsumer.OnStreamResponse(streamID, req, resp)
}

// OnFetchRequest implements
// go-control-plane/pkg/server/Callbacks.OnFetchRequest
func (c *cachingConsumerStats) OnFetchRequest(req *envoyapi.DiscoveryRequest) {
	defer c.startRequest(fetchStream, req)
	c.cachingConsumer.OnFetchRequest(req)
}

// OnFetchResponse implements
// go-control-plane/pkg/server/Callbacks.OnFetchResponse
func (c cachingConsumerStats) OnFetchResponse(
	req *envoyapi.DiscoveryRequest,
	resp *envoyapi.DiscoveryResponse,
) {
	defer c.completeResponse(fetchStream, req, resp)
	c.cachingConsumer.OnFetchResponse(req, resp)
}

type streamState struct {
	nodeID  string
	proxy   string
	zone    string
	typeURL string

	requestStart time.Time

	responseComplete time.Time
	responseVersion  string
	responseNonce    string
}

func newStreamState(req *envoyapi.DiscoveryRequest) *streamState {
	if req.Node == nil {
		return &streamState{
			typeURL: req.GetTypeUrl(),
		}
	}

	proxyRef := proxyRefFromNode(req.Node)
	zone := ""
	if proxyRef.ZoneRef() != nil {
		zone = proxyRef.ZoneRef().Name()
	}

	return &streamState{
		nodeID:  req.Node.GetId(),
		proxy:   proxyRef.Name(),
		zone:    zone,
		typeURL: req.GetTypeUrl(),
	}
}

func (s *streamState) start(stater stats.Stats) {
	s.requestStart = timeSource.Now()

	if !s.responseComplete.IsZero() {
		stater.Timing(
			v2.ConfigInterval,
			s.requestStart.Sub(s.responseComplete),
			stats.NewKVTag(stats.NodeTag, s.nodeID),
			stats.NewKVTag(stats.ProxyTag, s.proxy),
			stats.NewKVTag(stats.ZoneTag, s.zone),
			stats.NewKVTag(v2.ConfigType, s.typeURL),
		)
		s.responseComplete = time.Time{}
	}
}

func (s *streamState) complete(stater stats.Stats, version, nonce string) {
	s.responseComplete = timeSource.Now()
	s.responseVersion = version
	s.responseNonce = nonce

	if !s.requestStart.IsZero() {
		stater.Timing(
			v2.ConfigLatency,
			s.responseComplete.Sub(s.requestStart),
			stats.NewKVTag(stats.NodeTag, s.nodeID),
			stats.NewKVTag(stats.ProxyTag, s.proxy),
			stats.NewKVTag(stats.ZoneTag, s.zone),
			stats.NewKVTag(v2.ConfigType, s.typeURL),
		)
	}
}

func (s *streamState) ack(stater stats.Stats) {
	s.count(stater, v2.ConfigValid)
}

func (s *streamState) nack(stater stats.Stats) {
	s.count(stater, v2.ConfigInvalid)
}

func (s *streamState) count(stater stats.Stats, configState string) {
	stater.Count(
		v2.Config,
		1,
		stats.NewKVTag(stats.NodeTag, s.nodeID),
		stats.NewKVTag(stats.ProxyTag, s.proxy),
		stats.NewKVTag(stats.ZoneTag, s.zone),
		stats.NewKVTag(v2.ConfigState, configState),
		stats.NewKVTag(v2.ConfigType, s.typeURL),
	)
}

type streamStateMap struct {
	mutex *sync.Mutex
	state map[int64]map[string]*streamState
}

func newStreamStateMap() *streamStateMap {
	return &streamStateMap{
		mutex: &sync.Mutex{},
		state: map[int64]map[string]*streamState{},
	}
}

func (m *streamStateMap) get(streamID int64, req *envoyapi.DiscoveryRequest) (*streamState, bool) {
	typeURL := req.GetTypeUrl()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	streamMap, ok := m.state[streamID]
	if !ok {
		streamMap = map[string]*streamState{}
		m.state[streamID] = streamMap
	}

	state, ok := streamMap[typeURL]
	if !ok {
		state := newStreamState(req)
		streamMap[typeURL] = state
		return state, true
	}

	return state, false
}

func (m *streamStateMap) remove(streamID int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.state, streamID)
}
