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
	"github.com/turbinelabs/cli"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
)

func cmd() *command.Cmd {
	r := &runner{}
	c := &command.Cmd{
		Name:        "tbn-xds-test-server",
		Summary:     "Envoy API test server",
		Usage:       "[OPTIONS]",
		Description: "Serves fixture responses.",
		Runner:      r,
	}

	flags := tbnflag.Wrap(&c.Flags)
	scoped := flags.Scope("envoy", "envoy configuration")
	r.ff = adapter.NewXDSFromFlags(flags, stats.NewFromFlags(scoped))
	console.Init(flags)

	return c
}

type runner struct {
	ff adapter.XDSFromFlags
}

func (r *runner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	s, err := r.ff.Make(poller.NewNopRegistrar())
	if err != nil {
		return cmd.Error(err)
	}
	s.Consume(poller.MkFixtureObjects())

	if err := s.Run(); err != nil {
		return cmd.Error(err)
	}

	return command.NoError()
}

func mkCLI() cli.CLI {
	return cli.New(
		constants.TbnPublicVersion,
		cmd(),
	)
}

func main() {
	mkCLI().Main()
}
