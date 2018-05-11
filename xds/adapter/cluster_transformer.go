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
	"fmt"
	"net/http"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/collector"
	"github.com/turbinelabs/rotor/xds/collector/v1"
)

type (
	// clusterTransformer converts envoy clusters into tbn clusters, resolving
	// dynamic resources, and collecting errors encountered along the way.
	clusterTransformer = func([]*envoyapi.Cluster) (api.Clusters, map[string][]error)

	staticClusterTransformer  = func(*envoyapi.Cluster) (*api.Cluster, []error)
	dynamicClusterTransformer = func(
		*envoyapi.Cluster,
		map[string]*envoyapi.Cluster,
	) (*api.Cluster, []error)
)

// newClusterTransformer returns a ClusterTransformer that will resolve
// correspondent clusters and instances, using the DiscoveryRequest thunk to
// issue requests to a V1 SDS or V2 EDS server, if necessary.
func newClusterTransformer(rf func() *envoyapi.DiscoveryRequest) clusterTransformer {
	dynamicTransformer := func(
		c *envoyapi.Cluster,
		cm map[string]*envoyapi.Cluster,
	) (*api.Cluster, []error) {
		mkClusterResolver := func(
			cs *envoycore.ConfigSource,
		) (collector.ClusterResolver, error) {
			return newClusterResolver(cs, rf, cm, collector.RandomInstanceSelector)
		}

		return mkDynamicCluster(c, mkClusterResolver)
	}

	p := &fnClusterTransformer{
		staticTransform:  mkStaticCluster,
		dynamicTransform: dynamicTransformer,
	}

	return p.transform
}

type fnClusterTransformer struct {
	staticTransform  staticClusterTransformer
	dynamicTransform dynamicClusterTransformer
}

func (ct *fnClusterTransformer) transform(
	cs []*envoyapi.Cluster,
) (api.Clusters, map[string][]error) {
	staticClusterMap := make(map[string]*envoyapi.Cluster)
	dynamicClusters := []*envoyapi.Cluster{}
	errorsMap := make(map[string][]error)

	for _, c := range cs {
		if c != nil {
			name := c.GetName()

			switch c.GetType() {
			case envoyapi.Cluster_STATIC, envoyapi.Cluster_STRICT_DNS, envoyapi.Cluster_LOGICAL_DNS:
				staticClusterMap[name] = c
			case envoyapi.Cluster_EDS:
				dynamicClusters = append(dynamicClusters, c)
			default:
				errorsMap[name] = append(
					errorsMap[name],
					fmt.Errorf(
						"Unknown Cluster_DiscoveryType: %s",
						c.GetType().String(),
					),
				)
			}

		}
	}

	tbnClusters := api.Clusters{}
	for _, sc := range staticClusterMap {
		c, errs := ct.staticTransform(sc)
		if len(errs) > 0 {
			name := sc.GetName()
			errorsMap[name] = append(errorsMap[name], errs...)
		}

		if c != nil {
			tbnClusters = append(tbnClusters, *c)
		}
	}

	for _, dc := range dynamicClusters {
		c, errs := ct.dynamicTransform(dc, staticClusterMap)
		if len(errs) > 0 {
			name := dc.GetName()
			errorsMap[name] = append(errorsMap[name], errs...)
		}

		if c != nil {
			tbnClusters = append(tbnClusters, *c)
		}
	}

	return tbnClusters, errorsMap
}

func mkStaticCluster(sc *envoyapi.Cluster) (*api.Cluster, []error) {
	if sc.GetName() == "" {
		return nil, []error{fmt.Errorf("Empty cluster name returned: %v", sc)}
	}

	is, errs := envoyAddrsToTbnInstances(sc.GetHosts())

	return &api.Cluster{
		Name:            sc.GetName(),
		RequireTLS:      sc.GetTlsContext() != nil,
		Instances:       is,
		CircuitBreakers: envoyToTbnCircuitBreakers(sc.GetCircuitBreakers()),
	}, errs
}

func mkDynamicCluster(
	dc *envoyapi.Cluster,
	mkClusterResolver func(*envoycore.ConfigSource) (collector.ClusterResolver, error),
) (*api.Cluster, []error) {
	if dc.GetName() == "" {
		return nil, []error{fmt.Errorf("Empty cluster name: %v", dc)}
	}

	if dc.GetEdsClusterConfig() == nil || dc.GetEdsClusterConfig().GetEdsConfig() == nil {
		return nil, []error{fmt.Errorf("No EdsClusterConfig defined: %v", dc)}
	}

	errs := []error{}
	resolve, err := mkClusterResolver(dc.GetEdsClusterConfig().GetEdsConfig())

	if err != nil {
		errs = append(errs, err)
	}

	if resolve == nil {
		return nil, errs
	}

	name := dc.GetName()
	if dc.GetEdsClusterConfig().GetServiceName() != "" {
		name = dc.GetEdsClusterConfig().GetServiceName()
	}

	is, iErrs := resolve(name)
	if len(iErrs) > 0 {
		errs = append(errs, iErrs...)
	}

	return &api.Cluster{
		Name:            dc.GetName(),
		RequireTLS:      dc.GetTlsContext() != nil,
		Instances:       is,
		CircuitBreakers: envoyToTbnCircuitBreakers(dc.GetCircuitBreakers()),
	}, errs
}

func newClusterResolver(
	configSource *envoycore.ConfigSource,
	rf func() *envoyapi.DiscoveryRequest,
	clusterMap map[string]*envoyapi.Cluster,
	selector func(api.Instances) api.Instance,
) (collector.ClusterResolver, error) {
	if configSource.GetApiConfigSource() == nil {
		return nil, fmt.Errorf(
			"Only ApiConfigSource supported. Received %s",
			configSource.String(),
		)
	}

	acs := configSource.GetApiConfigSource()
	i, err := resolveEDSInstance(acs.GetClusterNames(), selector, clusterMap)
	if err != nil {
		return nil, err
	}

	switch acs.GetApiType() {
	case envoycore.ApiConfigSource_REST_LEGACY:
		return v1.NewClusterResolver(i.Host, i.Port, http.DefaultClient.Get), nil

	case envoycore.ApiConfigSource_REST:
		return asClusterResolver(newRESTEndpointService(i.Key()), rf), nil

	case envoycore.ApiConfigSource_GRPC:
		es, err := newGRPCEndpointService(i.Key())
		if err != nil {
			return nil, err
		}

		return asClusterResolver(es, rf), nil
	}

	return nil, fmt.Errorf("Unrecognized ApiConfigSourceType: %s", acs.GetApiType().String())
}

func resolveEDSInstance(
	clusterNames []string,
	selector func(api.Instances) api.Instance,
	clusterMap map[string]*envoyapi.Cluster,
) (api.Instance, error) {
	is := api.Instances{}
	errs := []error{}
	for _, name := range clusterNames {
		if c, ok := clusterMap[name]; ok {
			cis, cerrs := envoyAddrsToTbnInstances(c.GetHosts())
			is = append(is, cis...)
			errs = append(errs, cerrs...)
		}
	}

	// We ignore errors if we were able to resolve at least one instance
	switch {
	case len(is) == 0 && len(errs) > 0:
		return api.Instance{}, errs[0]
	case len(is) == 0:
		return api.Instance{}, fmt.Errorf("No EDS hosts resolved for cluster")
	default:
		return selector(is), nil
	}
}
