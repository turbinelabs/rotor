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

package rotor

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/test/assert"
)

func TestUpdaterWithXDSClose(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)
	mockXDS := adapter.NewMockXDS(ctrl)

	gomock.InOrder(
		mockXDS.EXPECT().Stop(),
		mockUpdater.EXPECT().Close().Return(errors.New("boom")),
	)

	u := &updaterWithXDS{Updater: mockUpdater, xds: mockXDS}
	assert.ErrorContains(t, u.Close(), "boom")
}
