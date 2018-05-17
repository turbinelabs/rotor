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
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/differ"
)

// future implementations of changeOperation would allow delta-based SD regimes
// to add/remove instances directly

// changeOperation encapsulates API calls to change clusters with the
// possibility of merging consecutive operations into a single API
// call.
type changeOperation interface {
	// Returns true if the given changeOperation may be merged into this one.
	canMerge(changeOperation) bool

	// Returns a merged change operation (or nil if canMerge returns false).
	merge(changeOperation) changeOperation

	// Applies the given changes against the API.
	execute(*updater) error
}

// replaceClustersOperation represents the wholesale replacement of a
// set of clusters in the API. Creation and deletion of API clusters
// is controlled by the updater's DiffOpts.
type replaceClustersOperation struct {
	clusters []api.Cluster
}

func (r *replaceClustersOperation) canMerge(other changeOperation) bool {
	_, ok := other.(*replaceClustersOperation)
	return ok
}

func (r *replaceClustersOperation) merge(other changeOperation) changeOperation {
	if op, ok := other.(*replaceClustersOperation); ok {
		return op
	}

	return nil
}

func (r *replaceClustersOperation) execute(u *updater) error {
	_, err := differ.DiffAndPatch(u.differ, r.clusters, u.diffOpts)
	return err
}

var _ changeOperation = &replaceClustersOperation{}
