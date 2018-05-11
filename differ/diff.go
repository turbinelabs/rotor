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
	"bytes"
	"fmt"
	"log"
	"sort"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/nonstdlib/log/console"
)

// Diff models a Cluster modification to be applied
type Diff interface {
	// Checksum returns the current checksum of the Cluster to be modified
	Checksum() api.Checksum
	// ClusterKey returns the ClusterKey of the Cluster to be modified
	ClusterKey() api.ClusterKey
	// Patch applies the modification to the given Cluster service,
	// using the given checksum, returing a new Diff with a potentially
	// different Checksum or ClusterKey.
	Patch(svc service.Cluster, checksum api.Checksum) (Diff, error)
	// Produce a map suitable for serialization
	DisplayMap() map[string]interface{}
}

// NewDiffCreate creates a Diff representing creation of a Cluster
func NewDiffCreate(cluster api.Cluster) Diff {
	diff := diffCreate{cluster}
	return &diff
}

// NewDiffModify creates a Diff representing modification of a Cluster.
// The given Cluster Checksum must match the existing Checksum.
func NewDiffModify(cluster api.Cluster) Diff {
	diff := diffModify{cluster}
	return &diff
}

// NewDiffDelete creates a Diff representing deletion of a Cluster
// corresponding to the given ClusterKey. The given Cluster Checksum must
// match the existing Checksum.
func NewDiffDelete(clusterKey api.ClusterKey, checksum api.Checksum) Diff {
	diff := diffDelete{clusterKey, checksum}
	return &diff
}

// NewDiffAddInstance creates a Diff representing adding the given Instance to
// the Cluster corresponding to the given ClusterKey. The given Cluster
// Checksum must match the existing Checksum.
func NewDiffAddInstance(
	clusterKey api.ClusterKey,
	checksum api.Checksum,
	instance api.Instance,
) Diff {
	diff := diffAddInstance{clusterKey, checksum, instance}
	return &diff
}

// NewDiffRemoveInstance creates a Diff representing removing the given Instance
// to the Cluster corresponding to the given ClusterKey. The given Cluster
// Checksum must match the existing Checksum.
func NewDiffRemoveInstance(
	clusterKey api.ClusterKey,
	checksum api.Checksum,
	instance api.Instance,
) Diff {
	diff := diffRemoveInstance{clusterKey, checksum, instance}
	return &diff
}

// Diff produces a []Diff computed from two Cluster slices
func diff(
	zoneKey api.ZoneKey,
	current, proposed []api.Cluster,
	opts DiffOpts,
) []Diff {
	currentMap := make(map[string]api.Cluster, len(current))
	for _, cluster := range current {
		console.Debug().Printf("Cluster %s Exists", cluster.Name)
		currentMap[cluster.Name] = cluster
	}

	proposedSeen := make(map[string]bool, len(proposed))
	for _, cluster := range proposed {
		console.Debug().Printf("Cluster %s Proposed", cluster.Name)
		proposedSeen[cluster.Name] = true
	}

	diffs := make([]Diff, 0)

	// for predictable ordering of Diffs
	sort.Sort(api.ClusterByClusterKey(current))
	sort.Sort(api.ClusterByClusterKey(proposed))

	// for each in proposed, look at current, see if needs diffing
	for _, pCluster := range proposed {
		cCluster, ok := currentMap[pCluster.Name]
		if !ok {
			if opts.IgnoreCreate {
				console.Debug().Printf(
					"IgnoreCreate=true, not creating Cluster %s, (%d Instances)",
					pCluster.Name,
					len(pCluster.Instances),
				)
			} else {
				pCluster.ZoneKey = zoneKey
				diffs = append(diffs, NewDiffCreate(pCluster))
				console.Debug().Printf(
					"Creating Cluster %s, (%d Instances)",
					pCluster.Name,
					len(pCluster.Instances),
				)
			}
			continue
		}

		console.Debug().Printf("Current Cluster %s Size: %d", cCluster.Name, len(cCluster.Instances))
		console.Debug().Printf("Proposed Cluster %s Size: %d", pCluster.Name, len(pCluster.Instances))

		// TODO: is it worth breaking this down into Add/RemoveInstance calls?
		// Or is there some heuristic to determine how many differences there
		// should be before we just wipe the Cluster?
		if !cCluster.Instances.Equals(pCluster.Instances) {
			patched := pCluster
			patched.ClusterKey = cCluster.ClusterKey
			patched.Checksum = cCluster.Checksum
			patched.ZoneKey = zoneKey
			diffs = append(diffs, NewDiffModify(patched))
			console.Debug().Printf(
				"Modifying Cluster %s, (%d Instances)",
				patched.Name,
				len(patched.Instances),
			)
		}
	}

	// for each current, make sure in proposed, or delete
	for _, cCluster := range current {
		if !proposedSeen[cCluster.Name] {
			if opts.IncludeDelete {
				diffs = append(diffs, NewDiffDelete(cCluster.ClusterKey, cCluster.Checksum))
				console.Debug().Printf("Deleting Cluster %s", cCluster.Name)
			} else {
				console.Debug().Printf("IncludeDelete=false, not deleting Cluster %s", cCluster.Name)
			}
		}
	}

	var logger *log.Logger
	if opts.DryRun {
		logger = console.Info()
		logger.Println("---- DRY RUN -----")
		if len(diffs) > 0 {
			logger.Println("Would have applied the following diffs:")
		}
	} else {
		logger = console.Debug()
		if len(diffs) > 0 {
			logger.Println("Applying the following diffs:")
		}
	}

	if len(diffs) == 0 {
		logger.Println("No diffs to apply")
		return nil
	}

	codec := codec.NewYaml()
	for _, d := range diffs {
		var b bytes.Buffer
		codec.Encode(d.DisplayMap(), &b)
		logger.Println(b.String())
	}

	if opts.DryRun {
		return nil
	}

	return diffs
}

// call api to add/remove clusters/instances
func patch(svc service.Cluster, diffs []Diff) error {
	checksums := make(map[api.ClusterKey]api.Checksum)
	for _, diff := range diffs {
		checksum := diff.Checksum()

		// see if we already have a checksum, which would indicate
		// that we've already operated on this cluster in this patch.
		// in this case, use the checksum from the previous operation
		// rather than the Diff's checksum
		checksum, ok := checksums[diff.ClusterKey()]
		if !ok {
			// if there is no checksum, this is the first time we've operated on
			// this cluster in this patch, so use the checksum from the Diff
			checksum = diff.Checksum()
		}

		// replace diff with the result of the Patch. The new Diff may have
		// a different Checksum or ClusterKey
		diff, err := diff.Patch(svc, checksum)
		if err != nil {
			return err
		}
		// store the new Checksum, in case we need to operate on this Cluster
		// again as part of this patch.
		checksums[diff.ClusterKey()] = diff.Checksum()
	}
	return nil
}

type diffComplete struct {
	clusterKey api.ClusterKey
	checksum   api.Checksum
}

func (d diffComplete) Checksum() api.Checksum     { return d.checksum }
func (d diffComplete) ClusterKey() api.ClusterKey { return d.clusterKey }
func (d diffComplete) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	return diffComplete{d.clusterKey, checksum}, nil
}
func (d diffComplete) String() string {
	return fmt.Sprintf("DiffComplete{%s,%s}", d.clusterKey, d.checksum)
}
func (d diffComplete) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":      "complete",
		"cluster_key": d.clusterKey,
		"checksum":    d.checksum.Checksum,
	}
}

type diffCreate struct {
	cluster api.Cluster
}

func (d diffCreate) Checksum() api.Checksum     { return d.cluster.Checksum }
func (d diffCreate) ClusterKey() api.ClusterKey { return d.cluster.ClusterKey }
func (d diffCreate) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	cluster, err := svc.Create(d.cluster)
	if err == nil {
		return diffComplete{cluster.ClusterKey, cluster.Checksum}, nil
	}
	return nil, err
}
func (d diffCreate) String() string { return fmt.Sprintf("DiffCreate{%v}", d.cluster) }
func (d diffCreate) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":    "create",
		"instances": d.cluster.Instances,
		"name":      d.cluster.Name,
	}
}

type diffModify struct {
	cluster api.Cluster
}

func (d diffModify) Checksum() api.Checksum     { return d.cluster.Checksum }
func (d diffModify) ClusterKey() api.ClusterKey { return d.cluster.ClusterKey }
func (d diffModify) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	cluster := d.cluster
	cluster.Checksum = checksum
	cluster, err := svc.Modify(cluster)
	if err == nil {
		return diffComplete{cluster.ClusterKey, cluster.Checksum}, nil
	}
	return nil, err
}
func (d diffModify) String() string { return fmt.Sprintf("DiffModify{%v}", d.cluster) }
func (d diffModify) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":      "modify",
		"cluster_key": d.cluster.ClusterKey,
		"checksum":    d.cluster.Checksum.Checksum,
		"instances":   d.cluster.Instances,
		"name":        d.cluster.Name,
	}
}

type diffDelete struct {
	clusterKey api.ClusterKey
	checksum   api.Checksum
}

func (d diffDelete) Checksum() api.Checksum     { return d.checksum }
func (d diffDelete) ClusterKey() api.ClusterKey { return d.clusterKey }
func (d diffDelete) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	err := svc.Delete(d.clusterKey, checksum)
	if err == nil {
		return diffComplete{d.clusterKey, api.Checksum{}}, nil
	}
	return nil, err
}
func (d diffDelete) String() string {
	return fmt.Sprintf("DiffDelete{%s,%s}", d.clusterKey, d.checksum)
}
func (d diffDelete) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":      "delete",
		"cluster_key": d.clusterKey,
		"checksum":    d.checksum.Checksum,
	}
}

type diffAddInstance struct {
	clusterKey api.ClusterKey
	checksum   api.Checksum
	instance   api.Instance
}

func (d diffAddInstance) Checksum() api.Checksum     { return d.checksum }
func (d diffAddInstance) ClusterKey() api.ClusterKey { return d.clusterKey }
func (d diffAddInstance) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	cluster, err := svc.AddInstance(d.clusterKey, checksum, d.instance)
	if err == nil {
		return diffComplete{cluster.ClusterKey, cluster.Checksum}, nil
	}
	return nil, err
}
func (d diffAddInstance) String() string {
	return fmt.Sprintf("DiffDelete{%s,%s,{%v}}", d.clusterKey, d.checksum, d.instance)
}
func (d diffAddInstance) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":      "add_instance",
		"cluster_key": d.clusterKey,
		"checksum":    d.checksum.Checksum,
		"instance":    d.instance,
	}
}

type diffRemoveInstance struct {
	clusterKey api.ClusterKey
	checksum   api.Checksum
	instance   api.Instance
}

func (d diffRemoveInstance) Checksum() api.Checksum     { return d.checksum }
func (d diffRemoveInstance) ClusterKey() api.ClusterKey { return d.clusterKey }
func (d diffRemoveInstance) Patch(svc service.Cluster, checksum api.Checksum) (Diff, error) {
	cluster, err := svc.RemoveInstance(d.clusterKey, checksum, d.instance)
	if err == nil {
		return diffComplete{cluster.ClusterKey, cluster.Checksum}, nil
	}
	return nil, err
}
func (d diffRemoveInstance) String() string {
	return fmt.Sprintf("DiffDelete{%s,%s,{%v}}", d.clusterKey, d.checksum, d.instance)
}
func (d diffRemoveInstance) DisplayMap() map[string]interface{} {
	return map[string]interface{}{
		"action":      "remove_instance",
		"cluster_key": d.clusterKey,
		"checksum":    d.checksum.Checksum,
		"instance":    d.instance,
	}
}
