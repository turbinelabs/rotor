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

// Package v1 provides a means for providing service discovery information
// via an envoy CDS server or a static envoy configuration file.
// See "rotor help envoy-cds-v1-file" for usage.
package v1

import (
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/codec"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/flag/usage"
	"github.com/turbinelabs/rotor"
)

const (
	envoyV1FileDescription = `Watches the given JSON or YAML Envoy
configuration file and updates Clusters stored in the Turbine Labs API at
startup and whenever the file changes.

Uses the provided file to discover the configuration for the SDS cluster that
will be used to resolve any cluster that is defined as 'sds'.
`

	envoyV1RestDescription = `Connects to a running Envoy CDS server and
updates clusters stored in the Turbine Labs API at startup and periodically
thereafer.

The {{ul "clusters-nodes"}} argument defines the path(s) used to call the CDS API
(details below). If not provided, the wildcard ` + cdsPathRoot + ` is used.
The {{ul "sds-addr"}} is used to collect hosts for each cluster defined with an
'sds' type. If no sds clusters exist, {{ul "sds-addr"}} is ignored. If no
{{ul "sds-addr"}} is provided, any cluster with 'sds' type will be ignored.

{{bold "clusters-nodes"}}

A comma-delimited, unbounded list of (1) service cluster and service node
pairs, (2) service clusters or (3) any combination of (1) and (2). Format is:

   cluster1:node1,cluster2,cluster1:node2,cluster3

These correspond to the local cluster(s) that the Envoys we're collecting for
are running in, as well as their corresponding node names (i.e. the
"--service-cluster" and "--service-node" arguments used at Envoy startup). For
cases where only <service_cluster>s are provided, it's assumed that the CDS
being called responds to "GET ` + cdsPathRoot + `/<service_cluster>" with all
clusters within that <service_cluster>. If no {{ul "clusters-nodes"}} argument is
passed, "GET ` + cdsPathRoot + `" will be called and should return all
clusters in a Zone.
`
)

// RESTCmd configures the parameters needed for running rotor against a V1
// envoy CDS server
func RESTCmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	cmd := &command.Cmd{
		Name:        "exp-envoy-cds-v1",
		Summary:     "envoy CDS v1 collector [EXPERIMENTAL]",
		Usage:       "[OPTIONS]",
		Description: envoyV1RestDescription,
	}

	flags := tbnflag.Wrap(&cmd.Flags)

	runner := &restRunner{clustersNodes: tbnflag.NewStrings()}
	runner.updaterFlags = updaterFlags

	flags.HostPortVar(
		&runner.addr,
		"addr",
		tbnflag.HostPort{},
		usage.Required("The address ('host:port') of a running CDS server."),
	)

	flags.HostPortVar(
		&runner.sdsAddr,
		"sds-addr",
		tbnflag.HostPort{},
		"The address ('host:port') of a running SDS server.",
	)

	flags.Var(
		&runner.clustersNodes,
		"clusters-nodes",
		"A comma-delimited list of cluster/node pairs or clusters with which to call the"+
			" CDS V1 API. Of the form: \"<cluster>[:<node>],...\".",
	)

	cmd.Runner = runner

	return cmd
}

// FileCmd configures the parameters needed for running rotor against a V1
// envoy CDS defined in a JSON or YAML file.
func FileCmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	cmd := &command.Cmd{
		Name:        "exp-envoy-cds-v1-file",
		Summary:     "envoy CDS v1 file collector [EXPERIMENTAL]",
		Usage:       "[OPTIONS] file",
		Description: envoyV1FileDescription,
	}

	cmd.Runner = &fileRunner{
		codecFlags:   codec.NewFromFlags(tbnflag.Wrap(&cmd.Flags)),
		updaterFlags: updaterFlags,
	}

	return cmd
}
