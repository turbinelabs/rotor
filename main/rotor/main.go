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
	"flag"

	"github.com/turbinelabs/cli"
	"github.com/turbinelabs/rotor"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/plugins/aws"
	"github.com/turbinelabs/rotor/plugins/consul"
	envoyv1 "github.com/turbinelabs/rotor/plugins/envoy/v1"
	envoyv2 "github.com/turbinelabs/rotor/plugins/envoy/v2"
	"github.com/turbinelabs/rotor/plugins/file"
	"github.com/turbinelabs/rotor/plugins/kubernetes"
	"github.com/turbinelabs/rotor/plugins/marathon"
)

const desc = `
Collects cluster instance data and updates the Turbine Labs API. A variety of
service discovery backends are supported via sub- commands, each with their own
configuration options. The file collector can be used as a bridge for
unsupported backends.

If the --xds.enabled flag is set, rotor will serve Envoy v2 xDS. When
configured with the --api.key flag, the Turbine Labs API will be used as the
backing store.

If no API key is provided, rotor will serve xDS in "standalone" mode
(regardless of the presence or absence of the --xds.enable flag); it will serve
a single listener on the configured port, with a virtual host named for each
collected cluster.
`

func mkCLI() cli.CLI {
	globalFlags := tbnflag.Wrap(&flag.FlagSet{})
	console.Init(globalFlags)
	updaterFlags := rotor.NewUpdaterFromFlags(globalFlags)

	c := cli.NewWithSubCmds(
		desc,
		constants.TbnPublicVersion,
		aws.AWSCmd(updaterFlags),
		consul.Cmd(updaterFlags),
		aws.ECSCmd(updaterFlags),
		envoyv1.RESTCmd(updaterFlags),
		envoyv1.FileCmd(updaterFlags),
		envoyv2.Cmd(updaterFlags),
		file.Cmd(updaterFlags),
		kubernetes.Cmd(updaterFlags),
		marathon.Cmd(updaterFlags),
		nopCmd(updaterFlags),
	)

	c.SetFlags(globalFlags.Unwrap())

	return c
}

func main() {
	mkCLI().Main()
}
