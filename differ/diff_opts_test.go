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
	"testing"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/test/assert"
)

func TestDiffOptsFromFlagsWithPrefix(t *testing.T) {
	flagSet := tbnflag.NewTestFlagSet()

	diffOptsWithPrefix := DiffOptsFromFlags(flagSet.Scope("prefix.", ""))
	assert.False(t, diffOptsWithPrefix.IncludeDelete)
	assert.False(t, diffOptsWithPrefix.IgnoreCreate)
	assert.False(t, diffOptsWithPrefix.DryRun)

	flagSet.Parse([]string{"-prefix.ignore-create"})
	assert.False(t, diffOptsWithPrefix.IncludeDelete)
	assert.True(t, diffOptsWithPrefix.IgnoreCreate)
	assert.False(t, diffOptsWithPrefix.DryRun)
}

func TestDiffOptsFromFlagsWithoutPrefix(t *testing.T) {
	flagSet := tbnflag.NewTestFlagSet()
	diffOpts := DiffOptsFromFlags(flagSet)
	assert.False(t, diffOpts.IncludeDelete)
	assert.False(t, diffOpts.IgnoreCreate)
	assert.False(t, diffOpts.DryRun)

	flagSet.Parse([]string{"-include-delete"})
	assert.True(t, diffOpts.IncludeDelete)
	assert.False(t, diffOpts.IgnoreCreate)
	assert.False(t, diffOpts.DryRun)
}

func TestDiffOptsFromFlagsDryRun(t *testing.T) {
	flagSet := tbnflag.NewTestFlagSet()
	diffOpts := DiffOptsFromFlags(flagSet)
	assert.False(t, diffOpts.IncludeDelete)
	assert.False(t, diffOpts.IgnoreCreate)
	assert.False(t, diffOpts.DryRun)

	flagSet.Parse([]string{"-dry-run"})
	assert.False(t, diffOpts.IncludeDelete)
	assert.False(t, diffOpts.IgnoreCreate)
	assert.True(t, diffOpts.DryRun)
}
