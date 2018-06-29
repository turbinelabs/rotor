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
	"fmt"
	"os"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/turbinelabs/cli"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor/constants"
)

var (
	defaultAddr = "127.0.0.1:50000"
)

func init() {
	if addr, ok := os.LookupEnv("ROTOR_XDS_ADDR"); ok {
		defaultAddr = addr
	}
}

func cmd() *command.Cmd {
	r := &runner{}

	cmd := &command.Cmd{
		Name:        "rotor-test-client",
		Summary:     "Test client for the rotor",
		Usage:       "[OPTIONS]",
		Description: "Calls rotor for a given cluster/zone, dumps the response to stdout",
		Runner:      r,
	}

	flagset := tbnflag.Wrap(&cmd.Flags)

	flagset.StringVar(
		&r.addr,
		"addr",
		defaultAddr,
		"Specifies which address to connect to.",
	)

	flagset.StringVar(
		&r.cluster,
		"cluster",
		"",
		"The name of the envoy cluster",
	)

	flagset.StringVar(
		&r.zone,
		"zone",
		"",
		"The name of the zone",
	)

	return cmd
}

type runner struct {
	addr    string
	cluster string
	zone    string
}

func (r *runner) localize(req *envoyapi.DiscoveryRequest) *envoyapi.DiscoveryRequest {
	req.Node = &envoycore.Node{
		Cluster:  r.cluster,
		Locality: &envoycore.Locality{Zone: r.zone},
	}
	return req
}

func (r *runner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	marshaler := &jsonpb.Marshaler{Indent: "  "}
	conn, err := grpc.Dial(r.addr, grpc.WithInsecure())
	if err != nil {
		return cmd.Error(err)
	}
	defer conn.Close()

	cds := envoyapi.NewClusterDiscoveryServiceClient(conn)
	res, err := cds.FetchClusters(context.Background(), r.localize(&envoyapi.DiscoveryRequest{}))
	if err != nil {
		return cmd.Error(err)
	}
	fmt.Print("cds: ")
	if err := marshaler.Marshal(os.Stdout, res); err != nil {

		return cmd.Error(err)
	}
	fmt.Println()

	names := []string{}
	for _, any := range res.GetResources() {
		c := &envoyapi.Cluster{}
		if err := types.UnmarshalAny(&any, c); err != nil {
			return cmd.Error(err)
		}
		names = append(names, c.GetName())
	}

	eds := envoyapi.NewEndpointDiscoveryServiceClient(conn)
	res, err = eds.FetchEndpoints(
		context.Background(),
		r.localize(&envoyapi.DiscoveryRequest{ResourceNames: names}),
	)
	if err != nil {
		return cmd.Error(err)
	}
	fmt.Print("eds: ")
	if err := marshaler.Marshal(os.Stdout, res); err != nil {
		return cmd.Error(err)
	}
	fmt.Println()

	lds := envoyapi.NewListenerDiscoveryServiceClient(conn)
	res, err = lds.FetchListeners(
		context.Background(),
		r.localize(&envoyapi.DiscoveryRequest{}),
	)
	if err != nil {
		return cmd.Error(err)
	}
	fmt.Print("lds: ")
	if err := marshaler.Marshal(os.Stdout, res); err != nil {
		return cmd.Error(err)
	}
	fmt.Println()

	names = []string{}
	for _, any := range res.GetResources() {
		l := &envoyapi.Listener{}
		if err := types.UnmarshalAny(&any, l); err != nil {
			return cmd.Error(err)
		}
		names = append(names, l.GetName())
	}

	rds := envoyapi.NewRouteDiscoveryServiceClient(conn)
	res, err = rds.FetchRoutes(
		context.Background(),
		r.localize(&envoyapi.DiscoveryRequest{ResourceNames: names}),
	)
	if err != nil {
		return cmd.Error(err)
	}
	fmt.Print("rds: ")
	if err := marshaler.Marshal(os.Stdout, res); err != nil {
		return cmd.Error(err)
	}
	fmt.Println()

	return command.NoError()
}

func mkCLI() cli.CLI {
	return cli.New(constants.TbnPublicVersion, cmd())
}

func main() {
	mkCLI().Main()
}
