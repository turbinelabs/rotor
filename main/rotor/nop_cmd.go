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

package main

import (
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/rotor"
)

const (
	nopDesc    = "Run the collector as only an xDS server and request logging sync. "
	nopSummary = nopDesc + `
Commonly used when running a pool of rotor as standalone xDS servers, or
when co-locating rotor as an xDS sidecar.`
)

func nopCmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	return &command.Cmd{
		Name:        "xds-only",
		Summary:     nopDesc,
		Usage:       "",
		Description: nopSummary,
		Runner:      &nopRunner{updaterFlags},
	}
}

type nopRunner struct {
	updaterFlags rotor.UpdaterFromFlags
}

func (r nopRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := r.updaterFlags.ValidateXDSOnly(); err != nil {
		return cmd.BadInput(err)
	}
	xds, err := r.updaterFlags.MakeXDS()
	if err != nil {
		return cmd.Error(err)
	}
	if err := xds.Run(); err != nil {
		return cmd.Error(err)
	}
	return command.NoError()
}
