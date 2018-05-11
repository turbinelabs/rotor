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
	"testing"

	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/test/assert"
)

func TestFileCmd(t *testing.T) {
	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(nil)

	cmd := FileCmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*fileRunner)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
	assert.NonNil(t, runner.codecFlags)
}

func TestRESTCmd(t *testing.T) {
	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(nil)

	cmd := RESTCmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*restRunner)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
	assert.NonNil(t, runner.clustersNodes)
}
