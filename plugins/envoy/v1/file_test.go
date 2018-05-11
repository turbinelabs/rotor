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

package v1

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/test/assert"
)

func TestFileRunnerInvalidUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockUpdaterFlags}

	err := errors.New("boom")
	mockUpdaterFlags.EXPECT().Validate().Return(err)

	cmdErr := fr.Run(FileCmd(mockUpdaterFlags), nil)
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("envoy-cds-v1-file: %s", err))
}

func TestFileRunnerInvalidCodecFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockCodecFlags := codec.NewMockFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockUpdaterFlags, codecFlags: mockCodecFlags}

	err := errors.New("boom")
	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockCodecFlags.EXPECT().Validate().Return(err)

	cmdErr := fr.Run(FileCmd(mockUpdaterFlags), nil)
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("envoy-cds-v1-file: %s", err))
}

func TestFileRunnerBadUpdater(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockCodecFlags := codec.NewMockFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockUpdaterFlags, codecFlags: mockCodecFlags}

	err := errors.New("boom")
	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockCodecFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFlags.EXPECT().Make().Return(nil, err)

	cmdErr := fr.Run(FileCmd(mockUpdaterFlags), []string{"/path/to/file"})
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("envoy-cds-v1-file: %s", err))
}
