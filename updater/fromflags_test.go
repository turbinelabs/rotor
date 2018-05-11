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
	"testing"
	"time"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/test/assert"
)

func TestNewFromFlags(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset)
	flagset.Parse([]string{"-delay=1m"})
	ffImpl := ff.(*fromFlags)
	assert.NonNil(t, ffImpl.diffOpts)
	assert.Equal(t, ffImpl.delay, 60*time.Second)
}

func TestNewFromFlagsDefault(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset)
	ffImpl := ff.(*fromFlags)
	assert.Equal(t, ffImpl.delay, 30*time.Second)
}

func TestNewFromFlagsWithDefault(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset, WithDefaultDelay(5))
	ffImpl := ff.(*fromFlags)
	assert.Equal(t, ffImpl.delay, 5*time.Second)
}

func TestFromFlagsValidateDelay(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset)
	flagset.Parse([]string{"-delay=500ms"})
	assert.ErrorContains(t, ff.Validate(), "delay may not be less than 1 second")
}

func TestFromFlagsValidateSkipMinDelay(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset, SkipMinDelay())
	flagset.Parse([]string{"-delay=500ms"})
	ffImpl := ff.(*fromFlags)
	assert.Equal(t, ffImpl.delay, 500*time.Millisecond)
}
