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

package file

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/test/assert"
)

func TestCmd(t *testing.T) {
	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(nil)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*fileRunner)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
	assert.NonNil(t, runner.codecFlags)
}

func TestFileRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockFromFlags}

	err := errors.New("boom")
	mockFromFlags.EXPECT().Validate().Return(err)

	cmdErr := fr.Run(Cmd(mockFromFlags), nil)
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("file: %s", err))
}

func TestFileRunnerRunMissingFile(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockFromFlags}

	mockFromFlags.EXPECT().Validate().Return(nil)

	cmdErr := fr.Run(Cmd(mockFromFlags), nil)
	assert.True(t, strings.Contains(cmdErr.Message, "single file"))
}

func TestFileRunnerRunCodecFlagsValidateError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockCodecFlags := codec.NewMockFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockFromFlags, codecFlags: mockCodecFlags}

	err := errors.New("boom")
	mockFromFlags.EXPECT().Validate().Return(nil)
	mockCodecFlags.EXPECT().Validate().Return(err)

	cmdErr := fr.Run(Cmd(mockFromFlags), []string{"x"})
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("file: %s", err))
}

func TestFileRunnerRunUpdateMakeError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockCodecFlags := codec.NewMockFromFlags(ctrl)

	fr := fileRunner{updaterFlags: mockFromFlags, codecFlags: mockCodecFlags}

	err := errors.New("boom")
	mockFromFlags.EXPECT().Validate().Return(nil)
	mockFromFlags.EXPECT().Make().Return(nil, err)
	mockCodecFlags.EXPECT().Validate().Return(nil)

	cmdErr := fr.Run(Cmd(mockFromFlags), []string{"x"})
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("file: %s", err))
}
