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

// Package differ implements a mechanism for synchronization of the local
// service discovery regime to the Turbine Labs public API.
//
// The Differ mirrors the data from an existing service discovery regime into
// the Cluster service using the customer site as the source of truth. This
// should ideally involve a combination of full-Zone reconciliation and
// incremental patching of Diffs. Since these involve large-scale comparison
// of the two datasets, it's probably more important to make sure multiple
// agents either don't operate at the same time, or can operate safely
// concurrently.
package differ

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
)

// Differ allows diffing and patching between the current Clusters and a
// proposed slice.
type Differ interface {
	// Diff returns a slice of Diffs representing the changes necessary to make
	// the Clusters in the Cluster service match the proposed slice of Clusters.
	Diff(proposed []api.Cluster, opts DiffOpts) ([]Diff, error)

	// Patch will apply a slice of Diffs to the Cluster service.
	Patch(diffs []Diff) error
}

// New returns a Differ backed by the given service.Cluster.
func New(svc service.Cluster, zoneKey api.ZoneKey) Differ {
	return svcDiffer{svc, zoneKey}
}

// DiffAndPatch uses the given Differ to add, modify, and remove Clusters in a
// given ZoneKey to match the given slice of Clusters. The slice of Diffs
// applied is returned.
func DiffAndPatch(
	d Differ,
	proposed []api.Cluster,
	opts DiffOpts,
) ([]Diff, error) {
	diffs, err := d.Diff(proposed, opts)
	if err != nil {
		return nil, err
	}

	err = d.Patch(diffs)
	if err != nil {
		return nil, err
	}

	return diffs, nil
}

type svcDiffer struct {
	svc     service.Cluster
	zoneKey api.ZoneKey
}

func (s svcDiffer) Diff(proposed []api.Cluster, opts DiffOpts) ([]Diff, error) {
	filter := service.ClusterFilter{ZoneKey: s.zoneKey}
	current, err := s.svc.Index(filter)
	if err != nil {
		return nil, err
	}
	return diff(s.zoneKey, current, proposed, opts), nil
}

func (s svcDiffer) Patch(diffs []Diff) error {
	return patch(s.svc, diffs)
}
