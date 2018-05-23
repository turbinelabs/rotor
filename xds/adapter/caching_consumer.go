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
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/pkg/server"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type cachingConsumer -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

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
	listenerConfig listenerAdapterConfig,
	caFile string,
	defaultTimeout time.Duration,
	resolveDNS bool,
) cachingConsumer {
	return registeringCachingConsumer{
		cache:     cache,
		registrar: registrar,
		adapt: newSnapshotAdapter(
			newEndpointAdapter(resolveDNS),
			newClusterAdapter(caFile),
			newRouteAdapter(defaultTimeout),
			newListenerAdapter(listenerConfig),
		),
		getObjects: func(svc service.All, proxy api.Proxy) (*poller.Objects, error) {
			return poller.NewRemote(svc).Objects(proxy.ProxyKey)
		},
	}
}

// registeringCachingConsumer signals its desire to consume objects by
// registering with a poller.Registrar
type registeringCachingConsumer struct {
	cache      snapshotCache
	registrar  poller.Registrar
	adapt      snapshotAdapter
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
func (c registeringCachingConsumer) OnStreamOpen(int64, string) {}

// OnStreamClosed implements
// go-control-plane/pkg/server/Callbacks.OnStreamClosed
func (c registeringCachingConsumer) OnStreamClosed(int64) {}

// OnStreamRequest implements
// go-control-plane/pkg/server/Callbacks.OnStreamRequest
func (c registeringCachingConsumer) OnStreamRequest(_ int64, req *v2.DiscoveryRequest) {
	c.OnFetchRequest(req)
}

// OnStreamResponse implements
// go-control-plane/pkg/server/Callbacks.OnStreamResponse
func (c registeringCachingConsumer) OnStreamResponse(
	_ int64,
	req *v2.DiscoveryRequest,
	resp *v2.DiscoveryResponse,
) {
	c.OnFetchResponse(req, resp)
}

// OnFetchRequest implements
// go-control-plane/pkg/server/Callbacks.OnFetchRequest
func (c registeringCachingConsumer) OnFetchRequest(req *v2.DiscoveryRequest) {
	console.Debug().Printf(`
-----------
  RECEIVED: %s
      NODE: %s
   CLUSTER: %s
  LOCALITY: %s
     NAMES: %s
     NONCE: %s
   VERSION: %s
`,
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
	console.Debug().Printf(
		"Responding with type: %s, version: %s, resources: %d",
		resp.GetTypeUrl(),
		resp.GetVersionInfo(),
		len(resp.GetResources()),
	)

	pRef := proxyRefFromNode(req.GetNode())
	ifLast := func() {
		console.Debug().Println("Last deregistration of", pRef.MapKey())
		c.cache.ClearSnapshot(pRef.MapKey())
	}
	if err := c.registrar.Deregister(pRef, ifLast); err != nil {
		console.Error().Printf("Error deregistering node(%s): %s", pRef.MapKey(), err)
	}
}
