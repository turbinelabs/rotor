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

package poller

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

type fromFlagsMocks struct {
	svc         *service.MockAll
	zone        *service.MockZone
	proxy       *service.MockProxy
	domain      *service.MockDomain
	route       *service.MockRoute
	sharedRules *service.MockSharedRules
	cluster     *service.MockCluster
	consumer    *MockConsumer
	registrar   *MockRegistrar
	stats       *stats.MockStats
	ff          fromFlags
	finish      func()
}

func mkFromFlagsMocks(t testing.TB) fromFlagsMocks {
	ctrl := gomock.NewController(assert.Tracing(t))

	ff := fromFlags{pollInterval: 1 * time.Second}

	return fromFlagsMocks{
		service.NewMockAll(ctrl),
		service.NewMockZone(ctrl),
		service.NewMockProxy(ctrl),
		service.NewMockDomain(ctrl),
		service.NewMockRoute(ctrl),
		service.NewMockSharedRules(ctrl),
		service.NewMockCluster(ctrl),
		NewMockConsumer(ctrl),
		NewMockRegistrar(ctrl),
		stats.NewMockStats(ctrl),
		ff,
		ctrl.Finish,
	}
}

func TestFromFlagsNoError(t *testing.T) {
	ff := fromFlags{pollInterval: 1 * time.Second}
	assert.Nil(t, ff.Validate())
}

func TestFromFlagsBadPollInterval(t *testing.T) {
	ff := fromFlags{pollInterval: 3 * time.Microsecond}
	assert.ErrorContains(t, ff.Validate(), "pollInterval must be greater than 500ms, was 3µs")
}

func TestFromFlagsMake(t *testing.T) {
	mocks := mkFromFlagsMocks(t)
	calls := make([]*gomock.Call, 0)

	defer func() {
		gomock.InOrder(calls...)
		mocks.ff.Make(mocks.svc, mocks.consumer, mocks.registrar, mocks.stats)
		mocks.finish()
	}()

	calls = append(calls,
		mocks.svc.EXPECT().SharedRules().Return(mocks.sharedRules),
		mocks.svc.EXPECT().Zone().Return(mocks.zone),
		mocks.svc.EXPECT().Proxy().Return(mocks.proxy),
		mocks.svc.EXPECT().Domain().Return(mocks.domain),
		mocks.svc.EXPECT().Route().Return(mocks.route),
		mocks.svc.EXPECT().Cluster().Return(mocks.cluster),
	)
}
