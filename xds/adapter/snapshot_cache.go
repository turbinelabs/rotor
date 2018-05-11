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
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"

	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type snapshotCache -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

// convenience interface for mocking
type snapshotCache interface {
	SetSnapshot(node string, snapshot cache.Snapshot) error
	ClearSnapshot(node string)
}

// tbnProxyNodeHash implements go-control-plane/pkg/cache/NodeHash
type tbnProxyNodeHash struct{}

// ID implements NodeHash.ID
func (tbnProxyNodeHash) ID(node *core.Node) string {
	pRef := proxyRefFromNode(node)
	if pRef == nil {
		return ""
	}
	return pRef.MapKey()
}

type consoleLogger struct{}

func (consoleLogger) Infof(format string, args ...interface{}) {
	console.Info().Printf(format, args...)
}

func (consoleLogger) Errorf(format string, args ...interface{}) {
	console.Error().Printf(format, args...)
}

// newSnapshotCache returns a configured cache.SnapshotCache
func newSnapshotCache() cache.SnapshotCache {
	return cache.NewSnapshotCache(false, tbnProxyNodeHash{}, consoleLogger{})
}

func proxyRefFromNode(node *core.Node) service.ProxyRef {
	if node == nil {
		return nil
	}

	var zoneName string
	if node.Locality != nil {
		zoneName = node.Locality.GetZone()
	}

	proxyName := node.GetCluster()
	zRef := service.NewZoneNameZoneRef(zoneName)
	return service.NewProxyNameProxyRef(proxyName, zRef)
}
