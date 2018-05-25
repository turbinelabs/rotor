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
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/turbinelabs/test/assert"
)

func TestTbnProxyNodeHashID(t *testing.T) {
	got := tbnProxyNodeHash{}.ID(&core.Node{
		Cluster: "the-proxy",
		Locality: &core.Locality{
			Zone: "the-zone",
		},
	})
	want := `{"proxy_name":"the-proxy","zone_name":"the-zone"}`
	assert.Equal(t, got, want)
}

func TestTbnProxyNodeHashIDNilNode(t *testing.T) {
	got := tbnProxyNodeHash{}.ID(nil)
	want := ""
	assert.Equal(t, got, want)
}

func TestTbnProxyNodeHashIDNilLocality(t *testing.T) {
	got := tbnProxyNodeHash{}.ID(&core.Node{
		Cluster: "the-proxy",
	})
	want := `{"proxy_name":"the-proxy","zone_name":"default-zone"}`
	assert.Equal(t, got, want)
}

func TestTbnProxyNodeHashIDEmptyZone(t *testing.T) {
	got := tbnProxyNodeHash{}.ID(&core.Node{
		Cluster:  "the-proxy",
		Locality: &core.Locality{},
	})
	want := `{"proxy_name":"the-proxy","zone_name":"default-zone"}`
	assert.Equal(t, got, want)
}

func TestTbnProxyNodeHashIDEmptyCluster(t *testing.T) {
	got := tbnProxyNodeHash{}.ID(&core.Node{
		Locality: &core.Locality{
			Zone: "the-zone",
		},
	})
	want := `{"proxy_name":"default-cluster","zone_name":"the-zone"}`
	assert.Equal(t, got, want)
}
