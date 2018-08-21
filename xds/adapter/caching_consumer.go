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
	"strings"
	"sync"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/pkg/server"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type cachingConsumer,streamRefsIface -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

// cachingConsumer implements poller.Consumer, consumes poller.Objects,
// receives callbacks from the go-control-plane server, and produces cache
// Snapshots.
type cachingConsumer interface {
	poller.Consumer
	server.Callbacks
}

// newCachingConsumer produces a new cachingConsumer
func newCachingConsumer(
	cache snapshotCache,
	registrar poller.Registrar,
	loggingCluster string,
	caFile string,
	defaultTimeout time.Duration,
	resolveDNS bool,
	provider staticResourcesProvider,
) cachingConsumer {
	return registeringCachingConsumer{
		cache:     cache,
		registrar: registrar,
		adapt: newSnapshotAdapter(
			newEndpointAdapter(resolveDNS),
			newClusterAdapter(caFile),
			newRouteAdapter(defaultTimeout),
			newListenerAdapter(loggingCluster),
			provider,
		),
		streamRefs: newStreamRefs(),
		getObjects: func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
			return poller.NewRemote(svc).Objects(proxy.ProxyKey)
		},
	}
}

type streamRefsIface interface {
	// Add the given proxy ref to the given stream
	Add(streamID int64, ref service.ProxyRef)
	// Remove the given proxy ref from the given stream
	Remove(streamID int64, ref service.ProxyRef)
	// RemoveAll removes all proxy refs, and returns a slice of refs to
	// deregister. A given ref will appear as many times in the slice as it was
	// added without being removed.
	RemoveAll(streamID int64) []service.ProxyRef
}

func newStreamRefs() streamRefsIface {
	return &streamRefs{
		refs: map[int64]map[string]*streamRefEntry{},
	}
}

// streamRefs tracks what streams are interested in which proxies.
type streamRefs struct {
	sync.Mutex
	refs map[int64]map[string]*streamRefEntry
}

type streamRefEntry struct {
	ref   service.ProxyRef
	count uint
}

func (r *streamRefs) Add(streamID int64, ref service.ProxyRef) {
	r.Lock()
	defer r.Unlock()
	k := ref.MapKey()

	// add the stream if necessary
	if r.refs[streamID] == nil {
		r.refs[streamID] = map[string]*streamRefEntry{}
	}

	// add the entry if necessary
	if r.refs[streamID][k] == nil {
		r.refs[streamID][k] = &streamRefEntry{ref: ref}
	}

	// increment the count
	r.refs[streamID][k].count++
}

func (r *streamRefs) Remove(streamID int64, ref service.ProxyRef) {
	r.Lock()
	defer r.Unlock()
	k := ref.MapKey()

	// nothing to do
	if r.refs[streamID] == nil {
		return
	}

	if r.refs[streamID][k] != nil {
		// decrement the count for this proxy ref
		r.refs[streamID][k].count--

		// remove the proxy ref entry from the stream if the count is zero
		if r.refs[streamID][k].count == 0 {
			delete(r.refs[streamID], k)
		}
	}

	// clean up the stream if no more entries
	if len(r.refs[streamID]) == 0 {
		delete(r.refs, streamID)
	}
}

func (r *streamRefs) RemoveAll(streamID int64) []service.ProxyRef {
	r.Lock()
	defer r.Unlock()

	var result []service.ProxyRef

	// nothing to do
	if r.refs[streamID] == nil {
		return result
	}

	// append each ref as many times as the value of the count
	for _, ref := range r.refs[streamID] {
		for i := uint(0); i < ref.count; i++ {
			result = append(result, ref.ref)
		}
	}

	// clean up the stream
	delete(r.refs, streamID)

	return result
}

// registeringCachingConsumer signals its desire to consume objects by
// registering with a poller.Registrar
type registeringCachingConsumer struct {
	cache      snapshotCache
	registrar  poller.Registrar
	adapt      snapshotAdapter
	streamRefs streamRefsIface
	getObjects func(service.All, api.Proxy) (*poller.Objects, error)
}

// Consume implements poller.Consumer
func (c registeringCachingConsumer) Consume(objs *poller.Objects) error {
	console.Debug().Println("Consuming", objs.Zone.Name, objs.Proxy.Name, objs.TerribleHash())
	pRef := service.NewProxyRef(objs.Proxy, objs.Zone)
	snapshot, err := c.adapt(objs)
	if err != nil {
		return err
	}

	return c.cache.SetSnapshot(pRef.MapKey(), snapshot)
}

// OnStreamOpen implements
// go-control-plane/pkg/server/Callbacks.OnStreamOpen
func (c registeringCachingConsumer) OnStreamOpen(streamID int64, streamType string) {
	console.Debug().Println("stream open: ", streamID, streamType)
}

// OnStreamClosed implements
// go-control-plane/pkg/server/Callbacks.OnStreamClosed
func (c registeringCachingConsumer) OnStreamClosed(streamID int64) {
	for _, ref := range c.streamRefs.RemoveAll(streamID) {
		c.deregister(ref)
	}
	console.Debug().Println("stream closed: ", streamID)
}

// OnStreamRequest implements
// go-control-plane/pkg/server/Callbacks.OnStreamRequest
func (c registeringCachingConsumer) OnStreamRequest(streamID int64, req *v2.DiscoveryRequest) {
	c.streamRefs.Add(streamID, proxyRefFromNode(req.GetNode()))
	c.onRequest(streamID, req)
}

// OnStreamResponse implements
// go-control-plane/pkg/server/Callbacks.OnStreamResponse
func (c registeringCachingConsumer) OnStreamResponse(
	streamID int64,
	req *v2.DiscoveryRequest,
	resp *v2.DiscoveryResponse,
) {
	c.onResponse(streamID, req, resp)
	c.streamRefs.Remove(streamID, proxyRefFromNode(req.GetNode()))
}

// OnFetchRequest implements
// go-control-plane/pkg/server/Callbacks.OnFetchRequest
func (c registeringCachingConsumer) OnFetchRequest(req *v2.DiscoveryRequest) {
	c.onRequest(-1, req)
}

func (c registeringCachingConsumer) onRequest(streamID int64, req *v2.DiscoveryRequest) {
	console.Debug().Printf(`
-----------
    STREAM: %d
  RECEIVED: %s
      NODE: %s
   CLUSTER: %s
  LOCALITY: %s
     NAMES: %s
     NONCE: %s
   VERSION: %s
`,
		streamID,
		req.GetTypeUrl(),
		req.GetNode().GetId(),
		req.GetNode().GetCluster(),
		req.GetNode().GetLocality(),
		strings.Join(req.GetResourceNames(), ", "),
		req.GetResponseNonce(),
		req.GetVersionInfo(),
	)

	pRef := proxyRefFromNode(req.GetNode())
	ifFirst := func(svc service.All, proxy api.Proxy) {
		console.Debug().Println("First registration of", pRef.MapKey())
		if objs, err := c.getObjects(svc, proxy); err != nil {
			console.Error().Printf(
				"Error getting initial objects for node(%s): %s",
				pRef.MapKey(),
				err,
			)
		} else if err := c.Consume(objs); err != nil {
			console.Error().Printf("Error consuming objects for node(%s): %s", pRef.MapKey(), err)
		}
	}
	if err := c.registrar.Register(pRef, ifFirst); err != nil {
		console.Error().Printf("Error registering node(%s): %s", pRef.MapKey(), err)
	}
}

// OnFetchResponse implements
// go-control-plane/pkg/server/Callbacks.OnFetchResponse
func (c registeringCachingConsumer) OnFetchResponse(
	req *v2.DiscoveryRequest,
	resp *v2.DiscoveryResponse,
) {
	c.onResponse(-1, req, resp)
}

func (c registeringCachingConsumer) onResponse(
	streamID int64,
	req *v2.DiscoveryRequest,
	resp *v2.DiscoveryResponse,
) {
	console.Debug().Printf(
		"Responding (%d) with type: %s, version: %s, resources: %d",
		streamID,
		resp.GetTypeUrl(),
		resp.GetVersionInfo(),
		len(resp.GetResources()),
	)

	pRef := proxyRefFromNode(req.GetNode())
	c.deregister(pRef)
}

func (c registeringCachingConsumer) deregister(pRef service.ProxyRef) {
	k := pRef.MapKey()
	ifLast := func() {
		console.Debug().Println("Last deregistration of", k)
		c.cache.ClearSnapshot(k)
	}
	if err := c.registrar.Deregister(pRef, ifLast); err != nil {
		console.Error().Printf("Error deregistering node(%s): %s", k, err)
	}
}
