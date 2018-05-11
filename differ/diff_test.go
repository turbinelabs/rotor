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

package differ

import (
	"fmt"
	"testing"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

var someErr = fmt.Errorf("just an error case")

func assertDiffClusterKey(t *testing.T, diff Diff, wantKey api.ClusterKey) {
	gotKey := diff.ClusterKey()
	assert.Equal(t, gotKey, wantKey)
}

func assertDiffChecksum(t *testing.T, diff Diff, wantCS api.Checksum) {
	gotCS := diff.Checksum()
	assert.Equal(t, gotCS, wantCS)
}

func testDiffModify(
	t *testing.T,
	mkDiff func(api.Cluster) (Diff, api.Cluster, map[string]interface{}),
) {
	_, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	cluster := api.Cluster{
		Name:      "foo",
		ZoneKey:   "Z1",
		Instances: []api.Instance{{Host: "foo", Port: 1234}},
	}
	diff, _, _ := mkDiff(cluster)

	assertDiffClusterKey(t, diff, key(""))
	assertDiffChecksum(t, diff, csum(""))

	// fails because of unknown object
	badCs := csum("ignored")
	switch ty := diff.(type) {
	case *diffDelete:
		svc.EXPECT().Delete(ty.clusterKey, badCs).Return(someErr)
	case *diffRemoveInstance:
		svc.EXPECT().RemoveInstance(ty.clusterKey, badCs, ty.instance).Return(api.Cluster{}, someErr)
	case *diffAddInstance:
		svc.EXPECT().AddInstance(ty.clusterKey, badCs, ty.instance).Return(api.Cluster{}, someErr)
	case *diffModify:
		modc := ty.cluster
		modc.Checksum = badCs
		svc.EXPECT().Modify(modc).Return(api.Cluster{}, someErr)
	default:
		assert.Failed(t, fmt.Sprintf("Did not match diff correctly: %T", diff))
	}

	_, err := diff.Patch(svc, badCs)
	assertDiffChecksum(t, diff, csum(""))
	assert.NonNil(t, err)

	// create it so that we can modify it
	created := cluster
	created.ClusterKey = key("ID-1")
	created.Checksum = csum("CS-1")

	diff, want, displayMap := mkDiff(cluster)

	assertDiffClusterKey(t, diff, cluster.ClusterKey)
	assertDiffChecksum(t, diff, cluster.Checksum)
	displayMap["checksum"] = cluster.Checksum.Checksum
	displayMap["cluster_key"] = cluster.ClusterKey
	assert.EqualJson(t, diff.DisplayMap(), displayMap)

	goodCs := created.Checksum
	switch ty := diff.(type) {
	case *diffDelete:
		svc.EXPECT().Delete(ty.clusterKey, goodCs).Return(nil)
	case *diffAddInstance:
		svc.EXPECT().AddInstance(ty.clusterKey, goodCs, ty.instance).Return(want, nil)
	case *diffRemoveInstance:
		svc.EXPECT().RemoveInstance(ty.clusterKey, goodCs, ty.instance).Return(want, nil)
	case *diffModify:
		modc := ty.cluster
		modc.Checksum = goodCs
		svc.EXPECT().Modify(modc).Return(want, nil)
	}
	diff, err = diff.Patch(svc, goodCs)

	assert.Nil(t, err)
	assertDiffChecksum(t, diff, want.Checksum)
	assert.EqualJson(t, diff.DisplayMap(), map[string]interface{}{
		"action":      "complete",
		"cluster_key": cluster.ClusterKey,
		"checksum":    want.Checksum.Checksum,
	})
}

func TestDiffCreate(t *testing.T) {
	_, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	cluster := api.Cluster{Name: "foo", ZoneKey: "Z1"}
	diff := NewDiffCreate(cluster)
	assertDiffClusterKey(t, diff, key(""))
	assertDiffChecksum(t, diff, csum(""))

	assert.EqualJson(t, diff.DisplayMap(), map[string]interface{}{
		"action":    "create",
		"instances": nil,
		"name":      "foo",
	})

	created := cluster
	created.ClusterKey = key("ID-1")
	created.Checksum = csum("CS-1")
	svc.EXPECT().Create(cluster).Return(created, nil)

	diff, err := diff.Patch(svc, csum("ignored"))
	assert.Nil(t, err)

	// the new ID is nominal proof that the cluster was created
	assertDiffClusterKey(t, diff, created.ClusterKey)
	assertDiffChecksum(t, diff, created.Checksum)

	assert.EqualJson(t, diff.DisplayMap(), map[string]interface{}{
		"action":      "complete",
		"cluster_key": "ID-1",
		"checksum":    "CS-1",
	})
}

func TestDiffModify(t *testing.T) {
	testDiffModify(t, func(cluster api.Cluster) (Diff, api.Cluster, map[string]interface{}) {
		want := cluster
		want.Name = "bar"
		diff := NewDiffModify(want)
		want.Checksum = csum("CS-2")
		return diff,
			want,
			map[string]interface{}{"action": "modify", "name": want.Name, "instances": want.Instances}
	})
}

func TestDiffAddInstance(t *testing.T) {
	var instance = api.Instance{Host: "bar", Port: 1235}
	testDiffModify(t, func(cluster api.Cluster) (Diff, api.Cluster, map[string]interface{}) {
		want := cluster
		want.Instances = []api.Instance{
			{Host: "foo", Port: 1234},
			instance,
		}
		want.Checksum = csum("CS-2")
		return NewDiffAddInstance(cluster.ClusterKey, cluster.Checksum, instance),
			want,
			map[string]interface{}{"action": "add_instance", "instance": instance}
	})
}

func TestDiffRemoveInstance(t *testing.T) {
	var instance = api.Instance{Host: "foo", Port: 1234}
	testDiffModify(t, func(cluster api.Cluster) (Diff, api.Cluster, map[string]interface{}) {
		want := cluster
		want.Instances = []api.Instance{}
		want.Checksum = csum("CS-2")
		return NewDiffRemoveInstance(cluster.ClusterKey, cluster.Checksum, instance),
			want,
			map[string]interface{}{"action": "remove_instance", "instance": instance}
	})
}

func TestDiffDelete(t *testing.T) {
	testDiffModify(t, func(cluster api.Cluster) (Diff, api.Cluster, map[string]interface{}) {
		return NewDiffDelete(cluster.ClusterKey, cluster.Checksum),
			api.Cluster{},
			map[string]interface{}{"action": "delete"}
	})
}
