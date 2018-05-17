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
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/test/assert"
)

var (
	testClusters      = []api.Cluster{{Name: "cluster1"}, {Name: "cluster2"}}
	emptyTestClusters = []api.Cluster{}
)

type getClustersResponse struct {
	clusters []api.Cluster
	err      error
}

type getClustersResponseIterator <-chan getClustersResponse

func startLooper(
	u Updater,
	s tbntime.Source,
	getClustersResponses getClustersResponseIterator,
) (stop func(), wait func()) {
	signalCh := make(chan os.Signal, 1)
	doneCh := make(chan struct{})

	looper := &updateLooper{
		time:     s,
		signalCh: signalCh,
	}

	getClusters := func() ([]api.Cluster, error) {
		resp := <-getClustersResponses
		return resp.clusters, resp.err
	}

	go func() {
		defer close(doneCh)
		defer close(signalCh)

		looper.run(u, getClusters)
	}()

	stopFunc := func() {
		signalCh <- os.Interrupt
	}

	waitFunc := func() {
		select {
		case <-doneCh:
		}
	}

	return stopFunc, waitFunc

}

func successIterator(ok ...api.Clusters) getClustersResponseIterator {
	ch := make(chan getClustersResponse, len(ok))
	for _, c := range ok {
		ch <- getClustersResponse{clusters: c}
	}
	return ch
}

func responseIterator(resp ...getClustersResponse) getClustersResponseIterator {
	ch := make(chan getClustersResponse, len(resp))
	for _, r := range resp {
		ch <- r
	}
	return ch
}

func TestUpdateLooper(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	signalCh := make(chan os.Signal, 1)
	defer close(signalCh)

	sync := make(chan struct{}, 1)

	mockUpdater := NewMockUpdater(ctrl)
	gomock.InOrder(
		mockUpdater.EXPECT().Replace(testClusters),
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Replace(testClusters),
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Close().Return(nil),
	)

	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		stop, wait := startLooper(mockUpdater, cs, successIterator(testClusters, testClusters))
		<-sync

		cs.Advance(time.Minute)
		<-sync

		stop()
		wait()
	})
}

func TestUpdateLooperEmptyCluster(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	signalCh := make(chan os.Signal, 1)
	defer close(signalCh)

	sync := make(chan struct{}, 1)

	mockUpdater := NewMockUpdater(ctrl)
	gomock.InOrder(
		mockUpdater.EXPECT().Replace(emptyTestClusters),
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Replace(testClusters),
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Close().Return(nil),
	)

	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		stop, wait := startLooper(mockUpdater, cs, successIterator(emptyTestClusters, testClusters))
		<-sync

		cs.Advance(time.Minute)
		<-sync

		stop()
		wait()
	})
}

func TestUpdateLooperError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	signalCh := make(chan os.Signal, 1)
	defer close(signalCh)

	sync := make(chan struct{}, 1)

	mockUpdater := NewMockUpdater(ctrl)
	gomock.InOrder(
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Replace(testClusters),
		mockUpdater.EXPECT().Delay().Return(time.Minute).Do(func() { sync <- struct{}{} }),
		mockUpdater.EXPECT().Close().Return(nil),
	)

	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		iter := responseIterator(
			getClustersResponse{nil, errors.New("boom")},
			getClustersResponse{testClusters, nil},
		)
		stop, wait := startLooper(mockUpdater, cs, iter)
		<-sync

		cs.Advance(time.Minute)
		<-sync

		stop()
		wait()
	})
}
