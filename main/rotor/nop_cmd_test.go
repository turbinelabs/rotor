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

package main

import (
	"errors"
	"os"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/test/assert"
)

func TestNopRunner(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockXDS := adapter.NewMockXDS(ctrl)
	mockXDS.EXPECT().Run().Return(nil)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().ValidateXDSOnly().Return(nil)
	mockUpdaterFromFlags.EXPECT().MakeXDS().Return(mockXDS, nil)

	cmd := nopCmd(mockUpdaterFromFlags)

	assert.Equal(t, cmd.Runner.Run(cmd, nil), command.NoError())
}

func TestNopRunnerErrors(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	cmd := nopCmd(mockUpdaterFromFlags)

	mockUpdaterFromFlags.EXPECT().ValidateXDSOnly().Return(errors.New("boom1"))
	err := cmd.Runner.Run(cmd, nil)
	assert.True(t, err.IsError())
	assert.StringContains(t, err.Message, "boom1")

	mockUpdaterFromFlags.EXPECT().ValidateXDSOnly().Return(nil)
	mockUpdaterFromFlags.EXPECT().MakeXDS().Return(nil, errors.New("boom2"))
	err = cmd.Runner.Run(cmd, nil)
	assert.True(t, err.IsError())
	assert.StringContains(t, err.Message, "boom2")

	mockXDS := adapter.NewMockXDS(ctrl)
	mockXDS.EXPECT().Run().Return(errors.New("boom3"))
	mockUpdaterFromFlags.EXPECT().ValidateXDSOnly().Return(nil)
	mockUpdaterFromFlags.EXPECT().MakeXDS().Return(mockXDS, nil)
	err = cmd.Runner.Run(cmd, nil)
	assert.True(t, err.IsError())
	assert.StringContains(t, err.Message, "boom3")
}

func TestRunXDSHandlesSignals(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockXDSResult := make(chan error, 1)
	mockXDSRunning := make(chan struct{}, 1)

	mockXDS := adapter.NewMockXDS(ctrl)
	gomock.InOrder(
		mockXDS.EXPECT().Run().DoAndReturn(func() error {
			mockXDSRunning <- struct{}{}
			return <-mockXDSResult
		}),
		mockXDS.EXPECT().Stop().Do(func() { mockXDSResult <- errors.New("boom") }),
	)

	signals := make(chan os.Signal, 1)

	done := make(chan error, 1)
	go func() {
		done <- runXDS(mockXDS, signals)
	}()

	// Wait for XDS to be running
	<-mockXDSRunning

	// Assert runXDS has not returned
	assert.ChannelEmpty(t, done)

	// Simulate a signal
	signals <- os.Interrupt

	assert.ErrorContains(t, <-done, "boom")
}
