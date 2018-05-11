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

package adapter

import (
	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// newGRPCDiscoveryService returns a clusterService backed by a GRPC CDS Server
func newGRPCClusterService(hostPortStr string) (clusterService, error) {
	conn, err := mkConnection(hostPortStr)
	if err != nil {
		return nil, err
	}

	cds := envoyapi.NewClusterDiscoveryServiceClient(conn)

	return clusterService(
		fnDiscoveryService{
			fetchFn: func(req *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
				return cds.FetchClusters(context.Background(), req)
			},
			closeFn: conn.Close,
		},
	), nil
}

// newGRPCEndpointService returns a endpointService backed by a GRPC EDS Server
func newGRPCEndpointService(hostPortStr string) (clusterService, error) {
	conn, err := mkConnection(hostPortStr)
	if err != nil {
		return nil, err
	}

	eds := envoyapi.NewEndpointDiscoveryServiceClient(conn)

	return endpointService(
		fnDiscoveryService{
			fetchFn: func(req *envoyapi.DiscoveryRequest) (*envoyapi.DiscoveryResponse, error) {
				return eds.FetchEndpoints(context.Background(), req)
			},
			closeFn: conn.Close,
		},
	), nil
}

func mkConnection(hostPortStr string) (*grpc.ClientConn, error) {
	return grpc.Dial(hostPortStr, grpc.WithInsecure())
}
