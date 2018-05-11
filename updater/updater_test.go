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

package updater

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/differ"
	"github.com/turbinelabs/test/assert"
)

var (
	emptyClusters = []api.Cluster{
		{ClusterKey: "key1", Name: "c1"},
		{ClusterKey: "key2", Name: "c2"},
	}

	c1Instances = api.Instances{
		api.Instance{
			Host: "c1h1",
			Port: 8000,
			Metadata: api.Metadata{
				{Key: "c1h1m1", Value: "c1h1v1"},
				{Key: "c1h1m2", Value: "c1h1v2"},
			},
		},
		api.Instance{
			Host: "c1h2",
			Port: 8001,
			Metadata: api.Metadata{
				{Key: "c1h2m1", Value: "c1h2v1"},
			},
		},
	}

	c2Instances = api.Instances{
		api.Instance{
			Host: "c2h1",
			Port: 8000,
			Metadata: api.Metadata{
				{Key: "c2h1m1", Value: "c2h1v1"},
			},
		},
	}

	c3Instances = api.Instances{
		api.Instance{
			Host: "c3h1",
			Port: 8000,
			Metadata: api.Metadata{
				{Key: "c3h1m1", Value: "c3h1v1"},
			},
		},
	}
)

type fakeChangeOperation struct {
	executeError   error
	executeCalls   int
	canMergeResult bool
	mergeResult    changeOperation
	mergeCalls     int
}

func (f *fakeChangeOperation) execute(u *updater) error {
	f.executeCalls++
	return f.executeError
}

func (f *fakeChangeOperation) canMerge(other changeOperation) bool {
	return f.canMergeResult
}

func (f *fakeChangeOperation) merge(other changeOperation) changeOperation {
	f.mergeCalls++
	return f.mergeResult
}

func makeUpdater(lastUpdate time.Time) *updater {
	u := New(nil, 30*time.Second, differ.DiffOpts{}, "")
	u.lastUpdate = lastUpdate
	return u
}

func TestUpdaterReplace(t *testing.T) {
	u := New(nil, 30*time.Second, differ.DiffOpts{}, "")

	updateApiCalls := 0
	u.updateApi = func(_ *updater) {
		updateApiCalls++
	}

	clusters := []api.Cluster{
		{Name: "c1", Instances: c1Instances},
		{Name: "c2", Instances: c2Instances},
	}

	u.Replace(clusters)

	assert.DeepEqual(
		t,
		u.changeOps,
		[]changeOperation{
			&replaceClustersOperation{clusters: clusters},
		},
	)

	assert.Equal(t, updateApiCalls, 1)
}

func TestUpdaterUpdateApi(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	fakeOp := &fakeChangeOperation{}

	u.changeOps = []changeOperation{fakeOp}

	updateApiAfterCalls := 0
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
	}

	u.updateApi(u)

	assert.Equal(t, fakeOp.executeCalls, 1)
	assert.Equal(t, len(u.changeOps), 0)
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 0)
}

func TestUpdaterUpdateApiMergesOps(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	mergedOp := &fakeChangeOperation{}
	fakeOp1 := &fakeChangeOperation{canMergeResult: true, mergeResult: mergedOp}
	fakeOp2 := &fakeChangeOperation{}

	u.changeOps = []changeOperation{fakeOp1, fakeOp2}

	updateApiAfterCalls := 0
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
	}

	u.updateApi(u)

	assert.Equal(t, fakeOp1.mergeCalls, 1)
	assert.Equal(t, fakeOp2.mergeCalls, 0)
	assert.Equal(t, mergedOp.executeCalls, 1)
	assert.Equal(t, len(u.changeOps), 0)
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 0)
}

func TestUpdaterUpdateApiIteratesOps(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	fakeOp1 := &fakeChangeOperation{canMergeResult: false}
	fakeOp2 := &fakeChangeOperation{}

	u.changeOps = []changeOperation{fakeOp1, fakeOp2}

	updateApiAfterCalls := 0
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
	}

	u.updateApi(u)

	assert.Equal(t, fakeOp1.executeCalls, 1)
	assert.Equal(t, fakeOp2.executeCalls, 1)
	assert.Equal(t, len(u.changeOps), 0)
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 0)
}

func TestUpdaterUpdateApiDelayed(t *testing.T) {
	lastUpdate := time.Now().Add(-1 * time.Second)

	u := makeUpdater(lastUpdate)

	fakeOp := &fakeChangeOperation{executeError: errors.New("boom")}

	u.changeOps = []changeOperation{fakeOp}

	updateApiAfterCalls := 0
	var updateApiAfterDelay time.Duration
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
		updateApiAfterDelay = delay
	}

	u.updateApi(u)

	assert.Equal(t, fakeOp.executeCalls, 0)
	assert.DeepEqual(t, u.changeOps, []changeOperation{fakeOp})
	assert.Equal(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 1)
	assert.LessThanEqual(t, updateApiAfterDelay, 30*time.Second)
	assert.GreaterThan(t, updateApiAfterDelay, 0)
}

func TestUpdaterUpdateApiDelayedOnError(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	fakeOp1 := &fakeChangeOperation{}
	fakeOp2 := &fakeChangeOperation{executeError: errors.New("boom")}
	fakeOp3 := &fakeChangeOperation{}

	u.changeOps = []changeOperation{fakeOp1, fakeOp2, fakeOp3}

	updateApiAfterCalls := 0
	var updateApiAfterDelay time.Duration
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
		updateApiAfterDelay = delay
	}

	u.updateApi(u)

	assert.Equal(t, fakeOp1.executeCalls, 1)
	assert.Equal(t, fakeOp2.executeCalls, 1)
	assert.Equal(t, fakeOp3.executeCalls, 0)
	assert.DeepEqual(t, u.changeOps, []changeOperation{fakeOp2, fakeOp3})
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 1)
	assert.Equal(t, updateApiAfterDelay, 30*time.Second)
}

func TestUpdaterUpdateApiDelayedOnMergedOpError(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	mergedOp := &fakeChangeOperation{executeError: errors.New("boom")}

	fakeOp1 := &fakeChangeOperation{canMergeResult: true, mergeResult: mergedOp}
	fakeOp2 := &fakeChangeOperation{}
	fakeOp3 := &fakeChangeOperation{}

	u.changeOps = []changeOperation{fakeOp1, fakeOp2, fakeOp3}

	updateApiAfterCalls := 0
	var updateApiAfterDelay time.Duration
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
		updateApiAfterDelay = delay
	}

	u.updateApi(u)

	assert.Equal(t, mergedOp.executeCalls, 1)
	assert.Equal(t, fakeOp1.executeCalls, 0)
	assert.Equal(t, fakeOp2.executeCalls, 0)
	assert.Equal(t, fakeOp3.executeCalls, 0)
	assert.DeepEqual(t, u.changeOps, []changeOperation{fakeOp1, fakeOp2, fakeOp3})
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 1)
	assert.Equal(t, updateApiAfterDelay, 30*time.Second)
}

func TestUpdaterUpdateApiDelayedOnErrorAfterMergedOp(t *testing.T) {
	lastUpdate := time.Now().Add(-60 * time.Second)

	u := makeUpdater(lastUpdate)

	mergedOp := &fakeChangeOperation{}

	fakeOp1 := &fakeChangeOperation{canMergeResult: true, mergeResult: mergedOp}
	fakeOp2 := &fakeChangeOperation{}
	fakeOp3 := &fakeChangeOperation{executeError: errors.New("boom")}

	u.changeOps = []changeOperation{fakeOp1, fakeOp2, fakeOp3}

	updateApiAfterCalls := 0
	var updateApiAfterDelay time.Duration
	u.updateApiAfter = func(_ *updater, delay time.Duration) {
		updateApiAfterCalls++
		updateApiAfterDelay = delay
	}

	u.updateApi(u)

	assert.Equal(t, mergedOp.executeCalls, 1)
	assert.Equal(t, fakeOp1.executeCalls, 0)
	assert.Equal(t, fakeOp2.executeCalls, 0)
	assert.Equal(t, fakeOp3.executeCalls, 1)
	assert.DeepEqual(t, u.changeOps, []changeOperation{fakeOp3})
	assert.NotEqual(t, u.lastUpdate, lastUpdate)
	assert.Equal(t, updateApiAfterCalls, 1)
	assert.Equal(t, updateApiAfterDelay, 30*time.Second)
}

func TestUpdaterUpdateApiAfter(t *testing.T) {
	u := New(nil, 30*time.Second, differ.DiffOpts{}, "")
	updateApiCalls := 0

	var wg sync.WaitGroup
	wg.Add(1)

	u.updateApi = func(_ *updater) {
		updateApiCalls++
		wg.Done()
	}

	u.updateApiAfter(u, 10*time.Millisecond)

	wg.Wait()
	assert.Equal(t, updateApiCalls, 1)
}

func TestUpdaterCancelUpdateTimer(t *testing.T) {
	u := New(nil, 30*time.Second, differ.DiffOpts{}, "")

	u.updateApi = func(_ *updater) {}

	u.updateApiAfter(u, 60*time.Second)

	timer := u.timer

	u.cancelUpdateTimer()
	assert.Nil(t, u.timer)

	// timer was canceled
	assert.False(t, timer.Reset(1))
}

func TestUpdaterDelay(t *testing.T) {
	u := New(nil, 99*time.Second, differ.DiffOpts{}, "")
	assert.Equal(t, u.Delay(), 99*time.Second)
}

func TestReplaceClustersOpCanMerge(t *testing.T) {
	op1 := &replaceClustersOperation{}
	op2 := &replaceClustersOperation{clusters: emptyClusters}
	fakeOp := &fakeChangeOperation{}

	assert.True(t, op1.canMerge(op2))
	assert.False(t, op1.canMerge(fakeOp))
}

func TestReplaceClustersOpMerge(t *testing.T) {
	op1 := &replaceClustersOperation{}
	op2 := &replaceClustersOperation{clusters: emptyClusters}
	fakeOp := &fakeChangeOperation{}

	assert.DeepEqual(t, op1.merge(op2), op2)
	assert.DeepEqual(t, op2.merge(op1), op1)
	assert.Nil(t, op1.merge(fakeOp))
}

func TestReplaceClustersOpExecute(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	mockDiffer := differ.NewMockDiffer(ctrl)

	u := New(mockDiffer, 30*time.Second, differ.DiffOpts{}, "")

	clusters := []api.Cluster{
		{Name: "c1", Instances: c1Instances},
		{Name: "c2", Instances: c2Instances},
	}

	op := &replaceClustersOperation{clusters: clusters}

	diffs := []differ.Diff{}
	mockDiffer.EXPECT().Diff(clusters, differ.DiffOpts{}).Return(diffs, nil)
	mockDiffer.EXPECT().Patch(diffs).Return(nil)

	assert.Nil(t, op.execute(u))

	ctrl.Finish()
}

func TestReplaceClustersOpExecuteError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	mockDiffer := differ.NewMockDiffer(ctrl)

	u := New(mockDiffer, 30*time.Second, differ.DiffOpts{}, "")

	clusters := []api.Cluster{
		{Name: "c1", Instances: c1Instances},
		{Name: "c2", Instances: c2Instances},
	}

	op := &replaceClustersOperation{clusters: clusters}

	err := errors.New("boom")
	mockDiffer.EXPECT().Diff(clusters, differ.DiffOpts{}).Return(nil, err)

	assert.DeepEqual(t, op.execute(u), err)

	ctrl.Finish()
}
