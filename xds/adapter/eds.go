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
	"errors"
	"fmt"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
)

const envoyLb = "envoy.lb"

// edsResourceAdapter turns poller.Objects into Endpoint cache.Resources
func edsResourceAdapter(objects *poller.Objects) (cache.Resources, error) {
	resources := make(map[string]cache.Resource, len(objects.Clusters))
	for _, cluster := range objects.Clusters {
		la := tbnClusterToEnvoyLoadAssignment(cluster)
		resources[la.GetClusterName()] = la
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
}

func tbnClusterToEnvoyLoadAssignment(tbnCluster tbnapi.Cluster) *envoyapi.ClusterLoadAssignment {
	lbEndpoints := []envoyendpoint.LbEndpoint{}
	for _, instance := range tbnCluster.Instances {
		lbEndpoint := envoyendpoint.LbEndpoint{
			Endpoint: &envoyendpoint.Endpoint{
				Address: mkEnvoyAddress(instance.Host, instance.Port),
			},
			HealthStatus: envoycore.HealthStatus_HEALTHY,
			Metadata:     toEnvoyMetadata(instance.Metadata),
		}

		lbEndpoints = append(lbEndpoints, lbEndpoint)
	}

	return &envoyapi.ClusterLoadAssignment{
		ClusterName: tbnCluster.Name,
		Endpoints:   []envoyendpoint.LocalityLbEndpoints{{LbEndpoints: lbEndpoints}},
	}
}

func envoyEndpointsToTbnInstances(
	lles []envoyendpoint.LocalityLbEndpoints,
) (tbnapi.Instances, []error) {
	if len(lles) == 0 {
		return tbnapi.Instances{}, nil
	}

	errs := []error{}
	is := tbnapi.Instances{}
	for _, lle := range lles {
		for _, le := range lle.GetLbEndpoints() {
			i, err := envoyEndpointToTbnInstance(le)
			if err != nil {
				fmtErr := fmt.Errorf(
					"Error making instances for endpoint %s: %s",
					le.String(),
					err.Error(),
				)
				errs = append(errs, fmtErr)

				continue
			}

			is = append(is, *i)
		}
	}

	return is, errs
}

func envoyAddrsToTbnInstances(hosts []*envoycore.Address) (tbnapi.Instances, []error) {
	is := tbnapi.Instances{}
	errs := []error{}
	for _, h := range hosts {
		i, err := envoyAddrToTbnInstance(h, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("Error creating Instance: %s", err))
			continue
		}
		is = append(is, *i)
	}

	return is, errs
}

func envoyEndpointToTbnInstance(le envoyendpoint.LbEndpoint) (*tbnapi.Instance, error) {
	ep := le.GetEndpoint()
	if ep == nil || ep.GetAddress() == nil {
		return nil, errors.New("Cannot convert empty Address to Instance")
	}

	return envoyAddrToTbnInstance(ep.GetAddress(), le.GetMetadata())
}

func envoyAddrToTbnInstance(
	addr *envoycore.Address,
	md *envoycore.Metadata,
) (*tbnapi.Instance, error) {
	host, port, err := envoyAddrToHostPort(addr)
	if err != nil {
		return nil, err
	}

	return &tbnapi.Instance{
		Host:     host,
		Port:     int(port),
		Metadata: envoyMetadataToTbnMetadata(md),
	}, nil

}

func envoyAddrToHostPort(addr *envoycore.Address) (string, uint32, error) {
	sa := addr.GetSocketAddress()
	if sa == nil {
		return "", 0, fmt.Errorf("Unsupported address type: %s", addr.String())
	}

	host := sa.GetAddress()
	if host == "" {
		return "", 0, fmt.Errorf("Invalid host for Address: %s", addr.String())
	}

	port := sa.GetPortValue()
	if port == 0 {
		return "", 0, fmt.Errorf("Invalid port for Address: %s", addr.String())
	}

	return host, port, nil
}

func envoyMetadataToTbnMetadata(md *envoycore.Metadata) tbnapi.Metadata {
	m := tbnapi.Metadata{}
	if md == nil || md.GetFilterMetadata() == nil {
		return m
	}

	if s, ok := md.GetFilterMetadata()[envoyLb]; ok {
		for key, value := range s.GetFields() {
			switch value.GetKind().(type) {
			case *types.Value_NullValue:
				m = append(m, tbnapi.Metadatum{Key: key, Value: ""})

			case *types.Value_NumberValue:
				m = append(m, tbnapi.Metadatum{
					Key:   key,
					Value: fmt.Sprintf("%g", value.GetNumberValue()),
				})

			case *types.Value_StringValue:
				m = append(m, tbnapi.Metadatum{
					Key:   key,
					Value: value.GetStringValue(),
				})

			case *types.Value_BoolValue:
				m = append(m, tbnapi.Metadatum{
					Key:   key,
					Value: fmt.Sprintf("%v", value.GetBoolValue()),
				})

			case *types.Value_StructValue, *types.Value_ListValue:
				console.Error().Printf("Skipping non-primitive Metadata key: %s", key)
				continue
			}
		}
	}

	return m
}
