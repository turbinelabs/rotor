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

import tbnflag "github.com/turbinelabs/nonstdlib/flag"

// DiffOpts describe desired behavior when producing a []Diff of two clusters.
type DiffOpts struct {
	IgnoreCreate  bool // If true, don't include cluster creation in the []Diff
	IncludeDelete bool // If true, include cluster deletion in the []Diff
	DryRun        bool // If true, don't apply destructive operations
}

// DiffOptsFromFlags install flags necessary to configure a DiffOpts into the
// provided FlagSet. Returns a pointer to the configured DiffOpts.
func DiffOptsFromFlags(flagset tbnflag.FlagSet) *DiffOpts {
	opts := &DiffOpts{}

	flagset.BoolVar(
		&opts.IgnoreCreate,
		"ignore-create",
		false,
		"If true, do not create new Clusters in the API",
	)

	flagset.BoolVar(
		&opts.IncludeDelete,
		"include-delete",
		false,
		"If true, delete missing Clusters from the API",
	)

	flagset.BoolVar(
		&opts.DryRun,
		"dry-run",
		false,
		"Log changes at the info level rather than submitting them to the API",
	)

	return opts
}
