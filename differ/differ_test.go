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
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/test/assert"
)

const zoneKey1 = api.ZoneKey("Z1")

func mkSvcDiffer(t *testing.T) (Differ, *service.MockCluster, func()) {
	ctrl := gomock.NewController(assert.Tracing(t))
	mc := service.NewMockCluster(ctrl)
	return New(mc, zoneKey1), mc, func() { ctrl.Finish() }
}

func mkMockSvcDiffer(t *testing.T) (*MockDiffer, func()) {
	ctrl := gomock.NewController(assert.Tracing(t))
	return NewMockDiffer(ctrl), func() { ctrl.Finish() }
}

func mkClusters(zoneKey api.ZoneKey, n int) []api.Cluster {
	result := make([]api.Cluster, n)
	for i := 0; i < n; i++ {
		result[i] = api.Cluster{
			ClusterKey: api.ClusterKey(fmt.Sprintf("ID-%d", i)),
			// starts because it's easier to track mutations than global cs generations
			Checksum: api.Checksum{Checksum: fmt.Sprintf("CS-1")},
			Name:     fmt.Sprintf("Cluster-%d", i),
			ZoneKey:  zoneKey,
			Instances: []api.Instance{
				{Host: fmt.Sprintf("Foo-%d", i), Port: 8000 + i},
				{Host: fmt.Sprintf("Bar-%d", i), Port: 8001 + i},
			},
		}
	}
	return result
}

func assertEqualClusters(t *testing.T, got []api.Cluster, want []api.Cluster) {
	if len(got) != len(want) {
		t.Errorf("got: %v, want %v", got, want)
		return
	}

	wantMap := make(map[api.ClusterKey]api.Cluster, len(want))

	for _, cluster := range want {
		wantMap[cluster.ClusterKey] = cluster
	}

	for _, gotCluster := range got {
		wantCluster, ok := wantMap[gotCluster.ClusterKey]

		if !ok {
			t.Errorf("got: {}, want %v", want)
			return
		}

		if !gotCluster.Equals(wantCluster) {
			t.Errorf("got: %v, want %v", gotCluster, wantCluster)
			return
		}
	}
}

func createClusters(svc service.Cluster, clusters []api.Cluster) ([]api.Cluster, error) {
	result := make([]api.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		cluster, err := svc.Create(cluster)
		if err != nil {
			return nil, err
		}
		result = append(result, cluster)
	}
	return result, nil
}

func key(k string) api.ClusterKey { return api.ClusterKey(k) }
func csum(cs string) api.Checksum { return api.Checksum{Checksum: cs} }

func TestDiffEmpty(t *testing.T) {
	differ, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	clusters := mkClusters("", 3)
	want := make([]Diff, 0, 3)
	for _, cluster := range clusters {
		cluster.ZoneKey = zoneKey1
		want = append(want, NewDiffCreate(cluster))
	}

	filter := service.ClusterFilter{ZoneKey: zoneKey1}

	svc.EXPECT().
		Index([]interface{}{filter}...).
		Return(nil, nil)

	got, err := differ.Diff(clusters, DiffOpts{})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, want)
}

func TestDiffNonEmpty(t *testing.T) {
	differ, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	filter := func(zk api.ZoneKey) service.ClusterFilter {
		return service.ClusterFilter{ZoneKey: zk}
	}

	storedClusters := mkClusters(zoneKey1, 2)
	// create a second set since modification of Instances below mutates the
	// original because of copy-by-value of pointer types.
	z1Clusters := mkClusters(zoneKey1, 3)

	svc.EXPECT().
		Index([]interface{}{filter(zoneKey1)}...).
		Return(storedClusters, nil).
		MaxTimes(5).
		MinTimes(5)

	// add a third cluster, should produce a create
	clusters := mkClusters("", 3)
	// slice out the first cluster, should produce a delete
	clusters = clusters[1:]
	// change metadata of second cluster, should produce a modify
	clusters[0].Instances[0].Metadata = api.Metadata{{Key: "foo", Value: "bar"}}

	// copy metadata into our copy of the second created instance
	wantModify := z1Clusters[1]
	wantModify.Instances[0].Metadata = clusters[0].Instances[0].Metadata

	wantCreate := z1Clusters[2]

	want := []Diff{
		NewDiffModify(wantModify),
		NewDiffCreate(wantCreate),
		NewDiffDelete(z1Clusters[0].ClusterKey, z1Clusters[0].Checksum),
	}

	got, err := differ.Diff(clusters, DiffOpts{IncludeDelete: true})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, want)

	got, err = differ.Diff(clusters, DiffOpts{IncludeDelete: true, DryRun: true})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, []Diff{})

	got, err = differ.Diff(clusters, DiffOpts{IgnoreCreate: true, IncludeDelete: true})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, []Diff{want[0], want[2]})

	got, err = differ.Diff(clusters, DiffOpts{IgnoreCreate: true})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, want[0:1])

	got, err = differ.Diff(clusters, DiffOpts{})
	assert.Nil(t, err)
	assert.HasSameElements(t, got, want[0:2])
}

func TestPatchKeyCollision(t *testing.T) {
	differ, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	diffs := []Diff{
		NewDiffDelete(key("foo"), csum("bar")),
		NewDiffDelete(key("foo"), csum("baz")),
	}

	gomock.InOrder(
		svc.EXPECT().Delete(key("foo"), csum("bar")).Return(nil),
		svc.EXPECT().Delete(key("foo"), api.Checksum{}).Return(someErr),
	)

	err := differ.Patch(diffs)
	assert.NonNil(t, err)
}

func TestPatch(t *testing.T) {
	differ, svc, finishFn := mkSvcDiffer(t)
	defer finishFn()

	// start with 5 clusters
	all := mkClusters(zoneKey1, 5)

	// create all but the first
	created := mkClusters(zoneKey1, 5)[1:]

	// copy the result into want, append the first
	want := make([]api.Cluster, 0, 5)
	want = append(want, created[0:3]...)
	want = append(want, all[0])

	// change the name for the modify
	want[0].Name = want[0].Name + "M"

	// add an instance
	instanceToAdd := api.Instance{Host: "foo", Port: 1234}
	want[1].Instances = append(want[1].Instances, instanceToAdd)

	// remove an instance
	instanceToRemove := want[2].Instances[1]
	want[2].Instances = want[2].Instances[0:1]

	diffs := []Diff{
		NewDiffCreate(want[3]),
		NewDiffModify(want[0]),
		NewDiffAddInstance(want[1].ClusterKey, want[1].Checksum, instanceToAdd),
		NewDiffRemoveInstance(want[2].ClusterKey, want[2].Checksum, instanceToRemove),
		NewDiffDelete(created[3].ClusterKey, created[3].Checksum),
	}

	// update the checksums and add the ID for the created Cluster
	want[3].ClusterKey = key("ID-5")
	want[3].Checksum = csum("CS-5")
	want[0].Checksum = csum("CS-6")
	want[1].Checksum = csum("CS-7")
	want[2].Checksum = csum("CS-8")

	modc := want[0]
	modc.Checksum = all[1].Checksum

	gomock.InOrder(
		svc.EXPECT().Create(all[0]).Return(want[3], nil),
		svc.EXPECT().Modify(modc).Return(want[0], nil),
		svc.EXPECT().
			AddInstance(all[2].ClusterKey, all[2].Checksum, instanceToAdd).
			Return(want[1], nil),
		svc.EXPECT().
			RemoveInstance(all[3].ClusterKey, all[3].Checksum, instanceToRemove).
			Return(want[2], nil),
		svc.EXPECT().
			Delete(all[4].ClusterKey, all[4].Checksum).
			Return(nil),
	)

	err := differ.Patch(diffs)
	assert.Nil(t, err)
}

func TestDiffAndPatchNoErr(t *testing.T) {
	differ, finishFn := mkMockSvcDiffer(t)
	defer finishFn()

	proposed := mkClusters(zoneKey1, 1)
	diffs := []Diff{NewDiffCreate(proposed[0])}

	gomock.InOrder(
		differ.EXPECT().Diff(proposed, DiffOpts{}).Return(diffs, nil),
		differ.EXPECT().Patch(diffs).Return(nil),
	)

	gotDiffs, gotErr := DiffAndPatch(differ, proposed, DiffOpts{})
	assert.DeepEqual(t, gotDiffs, diffs)
	assert.Nil(t, gotErr)
}

func TestDiffAndPatchDiffErr(t *testing.T) {
	differ, finishFn := mkMockSvcDiffer(t)
	defer finishFn()

	proposed := mkClusters(zoneKey1, 1)
	err := errors.New("foo bar")

	differ.EXPECT().Diff(proposed, DiffOpts{}).Return(nil, err)

	gotDiffs, gotErr := DiffAndPatch(differ, proposed, DiffOpts{})
	assert.Nil(t, gotDiffs)
	assert.Equal(t, gotErr, err)
}

func TestDiffAndPatchPatchErr(t *testing.T) {
	differ, finishFn := mkMockSvcDiffer(t)
	defer finishFn()

	proposed := mkClusters(zoneKey1, 1)
	diffs := []Diff{NewDiffCreate(proposed[0])}
	err := errors.New("foo bar")

	gomock.InOrder(
		differ.EXPECT().Diff(proposed, DiffOpts{}).Return(diffs, nil),
		differ.EXPECT().Patch(diffs).Return(err),
	)

	gotDiffs, gotErr := DiffAndPatch(differ, proposed, DiffOpts{})
	assert.Nil(t, gotDiffs)
	assert.Equal(t, gotErr, err)
}
