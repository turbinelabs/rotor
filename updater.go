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

package rotor

import (
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/adapter"
)

// updaterWithXDS wraps an updater.Updater and adapter.XDS and insures that when the
// Updater is closed, the XDS is stopped.
type updaterWithXDS struct {
	updater.Updater
	xds adapter.XDS
}

var _ updater.Updater = &updaterWithXDS{}

func (u *updaterWithXDS) Close() error {
	u.xds.Stop()
	return u.Updater.Close()
}
