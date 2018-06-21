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

// Package adapter provides adapters between Turbine Labs API objects and envoy
// xDS objects, in the form of an xDS server and ClusterCollector
// implementations
package adapter

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"time"

	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor/xds/poller"
)

// ResourceAdapter turns poller.Objects into cache.Resources
type resourceAdapter func(*poller.Objects) (cache.Resources, error)

// SnapshotAdapter turns poller.Objects into a cache.Snapshot
type snapshotAdapter func(*poller.Objects) (cache.Snapshot, error)

// NewSnapshotAdapter creates an SnapshotAdapter from the given ResourceAdapters.
func newSnapshotAdapter(
	adaptEndpoints resourceAdapter,
	adaptClusters resourceAdapter,
	adaptRoutes resourceAdapter,
	adaptListeners resourceAdapter,
) snapshotAdapter {
	return func(objs *poller.Objects) (cache.Snapshot, error) {
		endpoints, err := adaptEndpoints(objs)
		if err != nil {
			return cache.Snapshot{}, err
		}

		clusters, err := adaptClusters(objs)
		if err != nil {
			return cache.Snapshot{}, err
		}

		routes, err := adaptRoutes(objs)
		if err != nil {
			return cache.Snapshot{}, err
		}

		listeners, err := adaptListeners(objs)
		if err != nil {
			return cache.Snapshot{}, err
		}

		return cache.Snapshot{
			Endpoints: endpoints,
			Clusters:  clusters,
			Routes:    routes,
			Listeners: listeners,
		}, nil
	}
}

// newEndpointAdapter returns a resourceAdapter that produces Endpoint
// Resources.
func newEndpointAdapter(resolveDNS bool) resourceAdapter {
	if resolveDNS {
		return eds{resolveDNS: net.LookupIP}.edsResourceAdapter
	}
	return eds{}.edsResourceAdapter

}

// newClusterAdapter returns a resourceAdapter that produces Cluster Resources.
// If non-empty, the caFile string specifies a path for the certificate
// authority, which must be present on the Envoy serving traffic to these
// Clusters.
func newClusterAdapter(caFile string) resourceAdapter {
	return cds{caFile}.resourceAdapter
}

// newRouteAdapter returns a resourceAdapter that produces Route Resources. The
// defaultTimeout specifies the request timeout to be used for each RouteAction
// if no Route-specific timeout is configured.
func newRouteAdapter(defaultTimeout time.Duration) resourceAdapter {
	return rds{defaultTimeout}.resourceAdapter
}

// listenerAdapterConfig represents the configuration of a listener adapter.
type listenerAdapterConfig struct {
	loggingCluster string
}

// newListenerAdapter returns a resourceAdapter that produces Listener
// Resources. The config argument specifies either the LogFilePaths or the
// AccessLogService cluster name to be used when configuring logging for each
// Listener.
func newListenerAdapter(config listenerAdapterConfig) resourceAdapter {
	if config.loggingCluster != "" {
		return lds{loggingCluster: config.loggingCluster}.resourceAdapter
	}

	return lds{}.resourceAdapter
}

// constants used when handing out configs for other xDS resources,
// served by this service
const (
	defaultRoutingPriority  = envoycore.RoutingPriority_DEFAULT
	loadBalancerMetadataKey = "envoy.lb"
	xdsClusterName          = "tbn-xds"
	xdsRefreshDelaySecs     = 30
	httpsRedirectName       = "force-https"
)

var (
	xdsClusterConfig = envoycore.ConfigSource{
		ConfigSourceSpecifier: &envoycore.ConfigSource_ApiConfigSource{
			ApiConfigSource: &envoycore.ApiConfigSource{
				ApiType:      envoycore.ApiConfigSource_GRPC,
				ClusterNames: []string{xdsClusterName},
				RefreshDelay: ptr.Duration(xdsRefreshDelaySecs * time.Second),
			},
		},
	}
)

func mkEnvoyAddress(host string, port int) *envoycore.Address {
	return &envoycore.Address{
		Address: &envoycore.Address_SocketAddress{
			SocketAddress: &envoycore.SocketAddress{
				Protocol: envoycore.TCP,
				Address:  host,
				PortSpecifier: &envoycore.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}

func mkStatsName(name string, port int) string {
	return fmt.Sprintf("%s-%d", name, port)
}

func toEnvoyMetadata(metadata tbnapi.Metadata) *envoycore.Metadata {
	if len(metadata) == 0 {
		return &envoycore.Metadata{}
	}

	fields := make(map[string]*types.Value, len(metadata))
	for _, metadatum := range metadata {
		value := escapeMetadata(metadatum.Value)
		fields[metadatum.Key] = valueString(value)
	}

	return &envoycore.Metadata{
		FilterMetadata: map[string]*types.Struct{
			loadBalancerMetadataKey: {Fields: fields},
		},
	}
}

// Determines if a redirect is a valid https for the provided domain name.
func isHTTPSRedirect(host string, rd tbnapi.Redirect) bool {
	nameOk := rd.Name == httpsRedirectName
	fromOk := rd.From == "(.*)"

	u, err := url.Parse(rd.To)
	// who knows what happened here, but it's not a redirect
	if err != nil {
		return false
	}
	hostPortOk := false
	// Url parsing is a bit weird when it comes to including capture groups.
	// Depending on whether there's a port specified, the '$1' will end up
	// in a different spot.
	switch {
	case u.Hostname() == "$host" || u.Hostname() == host:
		hostPortOk = strings.HasSuffix(u.Port(), "$1") && strings.Count(u.Port(), "$1") == 1

	case u.Hostname() == "$host$1" || u.Hostname() == fmt.Sprintf("%s$1", host):
		hostPortOk = u.Port() == ""
	}

	destPathOk := u.EscapedPath() == ""
	toOk := u.Scheme == "https" && hostPortOk && destPathOk

	typeOk := rd.RedirectType == tbnapi.PermanentRedirect

	hcsOk := false
	switch {
	case len(rd.HeaderConstraints) == 0:
		hcsOk = true

	case len(rd.HeaderConstraints) == 1:
		hc := rd.HeaderConstraints[0]
		hcsOk = hc.Name == "X-Forwarded-Proto" && hc.Value == "https" && hc.Invert

	default:
		hcsOk = false
	}

	return nameOk && fromOk && toOk && typeOk && hcsOk
}

func boolValue(b bool) *types.BoolValue {
	return &types.BoolValue{Value: b}
}

func uint32Value(v uint32) *types.UInt32Value {
	return &types.UInt32Value{Value: v}
}

func valueString(s string) *types.Value {
	return &types.Value{
		Kind: &types.Value_StringValue{StringValue: s},
	}
}

func intPtrToUint32Ptr(intPtr *int) *types.UInt32Value {
	if intPtr == nil {
		return nil
	}

	return &types.UInt32Value{Value: uint32(*intPtr)}
}

func uint32PtrToIntPtr(ui32 *types.UInt32Value) *int {
	if ui32 == nil {
		return nil
	}

	return ptr.Int(int(ui32.GetValue()))
}

func intPtrToDurationPtr(intPtr *int) *types.Duration {
	if intPtr == nil {
		return nil
	}

	return &types.Duration{
		Seconds: int64(*intPtr / 1000),
		Nanos:   int32((*intPtr % 1000) * int(time.Millisecond)),
	}
}

// durationPtrToIntPtr supports millisecond granularity, which has the following
// implications:
//   - a Duration greater than math.MaxInt32 milliseconds will return nil
//   - Nanos will be truncated at the millisecond boundary
func durationPtrToIntPtr(d *types.Duration) *int {
	if d == nil {
		return nil
	}

	secs := time.Duration(d.Seconds) * time.Second
	nanos := time.Duration(d.Nanos)

	final := (secs + nanos) / time.Millisecond
	if final > math.MaxInt32 {
		return nil
	}

	return ptr.Int(int(final))
}
