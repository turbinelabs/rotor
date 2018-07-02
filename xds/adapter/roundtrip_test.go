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
	"fmt"
	"sort"
	"testing"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/collector"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
)

type envoyClustersByName []*envoyapi.Cluster

var _ sort.Interface = envoyClustersByName{}

func (b envoyClustersByName) Len() int           { return len(b) }
func (b envoyClustersByName) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b envoyClustersByName) Less(i, j int) bool { return b[i].Name < b[j].Name }

func TestEnvoyToTbnRoundTrip(t *testing.T) {
	objects := poller.MkFixtureObjects()
	for idx := range objects.Clusters {
		objects.Clusters[idx].ClusterKey = ""
		objects.Clusters[idx].ZoneKey = ""
	}

	requestFactoryCallCnt := 0
	requestFactory := func() *envoyapi.DiscoveryRequest {
		requestFactoryCallCnt++
		return &envoyapi.DiscoveryRequest{
			Node: &envoycore.Node{
				Locality: &envoycore.Locality{Zone: objects.Zone.Name},
			},
		}
	}

	clusterAdapter := newClusterAdapter("")
	envoyResources, err := clusterAdapter.adapt(objects)
	assert.NonNil(t, envoyResources.Items)
	assert.Nil(t, err)

	envoyClusters := make([]*envoyapi.Cluster, 0, len(envoyResources.Items))
	for _, resource := range envoyResources.Items {
		if cluster, ok := resource.(*envoyapi.Cluster); !ok {
			assert.Failed(t, "could not cast resource to cluster")
		} else {
			envoyClusters = append(envoyClusters, cluster)
		}
	}

	sort.Sort(sort.Reverse(envoyClustersByName(envoyClusters)))

	// cluster resolver backed by fixture objects
	es := mkTestEndpointService(objects)
	cr := asClusterResolver(es, requestFactory)

	// Since we know that our CDS pins everything to an eds, make some
	// assertions on the ConfigSource provided
	mkClusterResolver := func(cs *envoycore.ConfigSource) (collector.ClusterResolver, error) {
		assert.NonNil(t, cs)

		acs := cs.GetApiConfigSource()
		assert.NonNil(t, acs)
		assert.Equal(t, acs.GetApiType(), envoycore.ApiConfigSource_GRPC)
		assert.ArrayEqual(t, acs.GetClusterNames(), []string{"tbn-xds"})
		assert.Equal(t, *acs.GetRefreshDelay(), 30*time.Second)

		return cr, nil
	}

	dynamicTransformer := func(
		c *envoyapi.Cluster,
		cm map[string]*envoyapi.Cluster,
	) (*api.Cluster, []error) {
		// our CDS never marks anything as static so this should be
		// empty, pending https://github.com/turbinelabs/tbn/issues/4599
		assert.Equal(t, len(cm), 0)
		return mkDynamicCluster(c, mkClusterResolver)
	}

	ct := &fnClusterTransformer{
		staticTransform:  mkStaticCluster,
		dynamicTransform: dynamicTransformer,
	}

	tbnCs, errMap := ct.transform(envoyClusters)
	assert.Equal(t, requestFactoryCallCnt, len(objects.Clusters))
	assert.Equal(t, len(tbnCs), len(objects.Clusters))
	assert.Equal(t, len(errMap), 0)

	for idx := range tbnCs {
		got := tbnCs[idx]
		want := objects.Clusters[idx]
		if !assert.True(t, got.Equals(want)) {
			fmt.Printf("got:  %#v\n", got)
			fmt.Printf("want: %#v\n", want)
		}
	}
}

func mkTestEndpointService(objects *poller.Objects) endpointService {
	resources, _ := newEndpointAdapter(false).adapt(objects)
	backingMap := make(map[string]*envoyapi.ClusterLoadAssignment, len(resources.Items))
	for _, resource := range resources.Items {
		if cla, ok := resource.(*envoyapi.ClusterLoadAssignment); ok {
			backingMap[cla.GetClusterName()] = cla
		}
	}

	fetch := func(req *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
		found := []*envoyapi.ClusterLoadAssignment{}
		for _, name := range req.GetResourceNames() {
			if name != "" && backingMap[name] != nil {
				found = append(found, backingMap[name])
			}
		}

		typeURL := ""
		resources := make([]types.Any, len(found))
		for idx, cla := range found {
			any, err := types.MarshalAny(cla)
			if err != nil {
				return nil, err
			}

			resources[idx] = *any
			typeURL = any.GetTypeUrl()
		}
		resp := &envoyapi.DiscoveryResponse{
			Resources: resources,
			TypeUrl:   typeURL,
		}

		return resp, nil
	}

	return endpointService(fnDiscoveryService{fetchFn: fetch})
}
