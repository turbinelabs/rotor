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

package v2

import (
	"time"

	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/flag/usage"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/rotor/xds/collector"
)

const envoyV2Description = `{{ul "EXPERIMENTAL"}} Connects to a running Envoy
CDS server and updates clusters stored in the Turbine Labs API at startup
and periodically thereafter.

Depending on parameters, uses JSON or GRPC to load clusters and will use
results to resolve corresponding instances statically or via configured v2 EDS or
 v1 SDS servers that are provided in CDS results.
`

// Cmd configures the parameters needed for running rotor against a V2
// envoy CDS server, over JSON or GRPC.
func Cmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	cmd := &command.Cmd{
		Name:        "exp-envoy-cds-v2",
		Summary:     "envoy CDS v2 collector [EXPERIMENTAL]",
		Usage:       "[OPTIONS]",
		Description: envoyV2Description,
	}

	flags := tbnflag.Wrap(&cmd.Flags)

	r := &runner{
		updaterFlags: updaterFlags,
		format:       tbnflag.NewChoice("grpc", "json").WithDefault("grpc"),
	}

	flags.HostPortVar(
		&r.addr,
		"addr",
		tbnflag.HostPort{},
		usage.Required("The address ('host:port') of a running CDS server."),
	)

	flags.Var(&r.format, "format", "Format of CDS being called.")

	cmd.Runner = r

	return cmd
}

type runner struct {
	updaterFlags rotor.UpdaterFromFlags
	addr         tbnflag.HostPort
	format       tbnflag.Choice
}

func (r *runner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := r.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	updater, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	isJSON := r.format.String() == "json"
	collect, err := adapter.NewClusterCollector(r.addr.Addr(), updater.ZoneName(), isJSON)
	if err != nil {
		return cmd.Error(err)
	}

	updateLoop(updater, collect)
	defer collect.Close()

	return command.NoError()
}

func updateLoop(u updater.Updater, c collector.ClusterCollector) {
	for {
		tbnClusters, errMap := c.Collect()
		if len(errMap) > 0 {
			reportErrs(errMap)
		}

		if len(tbnClusters) > 0 {
			u.Replace(tbnClusters)
		}

		time.Sleep(u.Delay())
	}
}

func reportErrs(errMap map[string][]error) {
	for c, errs := range errMap {
		for _, e := range errs {
			console.Error().Printf("Error handling CDS update for cluster %s: %s", c, e)
		}
	}
}
