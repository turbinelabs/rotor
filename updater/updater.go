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

// Package updater provides implements the application of a complete set of
// or observed changes to cluster data to the Turbine API with a minimum
// delay between updates.
package updater

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"log"
	"sync"
	"time"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/differ"
)

// Updater provies a mechanism for updating a set of Clusters within a Zone
// at specified interval.
type Updater interface {
	// Replace discards any unapplied changes (from previous
	// delayed operations).  It schedules changes to remove all
	// existing instances the given clusters and to add all
	// instances in the given clusters. Clusters are created or
	// deleted only if the appropriate flags are set when creating
	// the Updater. If sufficient time has elapsed since the last
	// update, the scheduled update is executed immediately.
	Replace(clusters []api.Cluster)

	// Delay returns the minimum delay between API updates.
	Delay() time.Duration

	// ZoneName returns the name of the zone we're managing updates for.
	ZoneName() string
}

type updater struct {
	differ   differ.Differ
	delay    time.Duration
	diffOpts differ.DiffOpts

	mutex      *sync.Mutex
	changeOps  []changeOperation
	lastUpdate time.Time
	timer      *time.Timer

	updateApi      func(*updater)
	updateApiAfter func(*updater, time.Duration)
	zoneName       string
}

var _ Updater = &updater{}

func New(
	differ differ.Differ,
	delay time.Duration,
	diffOpts differ.DiffOpts,
	zoneName string,
) *updater {
	return &updater{
		differ:         differ,
		delay:          delay,
		mutex:          &sync.Mutex{},
		changeOps:      []changeOperation{},
		diffOpts:       diffOpts,
		updateApi:      updateApi,
		updateApiAfter: updateApiAfter,
		zoneName:       zoneName,
	}
}

// Updates the API with the current set of changes. If insufficient
// time has elapsed since the last update to maintain the minimum time
// between updates, the update is deferred. Otherwise, cancels any
// deferred updates and applies the pending changes. Assumes mutex is
// locked.
func updateApi(u *updater) {
	elapsed := time.Since(u.lastUpdate)

	if elapsed >= u.delay {
		// update API immediately
		u.cancelUpdateTimer()
		u.lastUpdate = time.Now()

		for len(u.changeOps) > 0 {
			op := u.changeOps[0]
			i := 1
			for ; i < len(u.changeOps); i++ {
				nextOp := u.changeOps[i]

				if op.canMerge(nextOp) {
					op = op.merge(nextOp)
				} else {
					break
				}
			}

			err := op.execute(u)
			if err != nil {
				log.Printf("rotor: api update failed: %s", err.Error())
				u.updateApiAfter(u, u.delay)
				return
			}

			u.changeOps = u.changeOps[i:]
		}
	} else {
		u.updateApiAfter(u, u.delay-elapsed)
	}
}

// Defers an update for the given duration. Assumes mutex is locked
func updateApiAfter(u *updater, d time.Duration) {
	if u.timer == nil {
		u.timer = time.AfterFunc(d, func() { u.updateApi(u) })
	}
}

// Cancels a previously deferred update, if any. Assumes mutex is locked.
func (u *updater) cancelUpdateTimer() {
	if u.timer != nil {
		u.timer.Stop()
		u.timer = nil
	}
}

func (u *updater) Replace(clusters []api.Cluster) {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	u.changeOps = []changeOperation{
		&replaceClustersOperation{clusters: clusters},
	}

	u.updateApi(u)
}

func (u *updater) Delay() time.Duration {
	return u.delay
}

func (u *updater) ZoneName() string {
	return u.zoneName
}
