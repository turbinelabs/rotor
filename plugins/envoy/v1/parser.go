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

package v1

import (
	"fmt"
	"io"
	"net/http"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/collector"
)

func defaultResolverFactory() resolverFactory {
	return newResolverFactory(http.DefaultClient.Get, collector.RandomInstanceSelector)
}

func newFileParser(codec codec.Codec, rf resolverFactory) *parser {
	p := &parser{
		mkEnvoyClusters: func(r io.Reader) ([]cluster, collector.ClusterResolver, error) {
			config := &bootstrapConfig{}

			if err := codec.Decode(r, &config); err != nil {
				return nil, nil, err
			}

			resolver, err := rf(config.ClusterManager)
			if err != nil {
				return nil, nil, err
			}

			return config.ClusterManager.Clusters, resolver, nil
		},
	}

	return p
}

func newCdsParser(cr collector.ClusterResolver) *parser {
	codec := codec.NewJson()

	p := &parser{
		mkEnvoyClusters: func(r io.Reader) ([]cluster, collector.ClusterResolver, error) {
			cdsResp := &cdsResp{}

			err := codec.Decode(r, &cdsResp)
			if err != nil {
				return nil, nil, err
			}

			return cdsResp.Clusters, cr, nil
		},
	}

	return p
}

type parser struct {
	mkEnvoyClusters func(io.Reader) ([]cluster, collector.ClusterResolver, error)
}

func (p *parser) parse(r io.Reader) ([]api.Cluster, error) {
	envoyClusters, resolve, err := p.mkEnvoyClusters(r)
	if err != nil {
		return nil, err
	}

	clusters := map[string]api.Cluster{}
	for _, c := range envoyClusters {
		if _, exists := clusters[c.Name]; exists {
			return nil, fmt.Errorf("duplicate cluster: %s", c.Name)
		}

		instances := api.Instances{}
		switch {
		case c.Type == "sds" && resolve == nil:
			console.Error().Printf("No SDS defined. Skipping cluster %s", c.Name)
			continue

		case c.Type == "sds":
			sdsInstances, errs := resolve(c.ServiceName)
			if len(errs) > 0 {
				for _, e := range errs {
					console.Error().Printf("Error getting SDS instances: %v", e)
				}

				continue
			}

			instances = append(instances, sdsInstances...)

		default:
			for _, h := range c.Hosts {
				i, err := mkInstance(h.URL)
				if err != nil {
					console.Error().Printf("Invalid cluster host: %s", err)
				}

				instances = append(instances, i)
			}
		}

		hc, err := mkHealthChecks(c.HealthCheck)
		if err != nil {
			console.Error().Printf("Skipping health checks: %v", err)
		}

		cluster := api.Cluster{
			Name:             c.Name,
			Instances:        instances,
			CircuitBreakers:  mkCircuitBreakers(c.CircuitBreakers),
			OutlierDetection: mkOutlierDetection(c.OutlierDetection),
			HealthChecks:     hc,
		}

		clusters[cluster.Name] = cluster
	}

	result := make(api.Clusters, 0, len(clusters))
	for _, configCluster := range envoyClusters {
		if tbnCluster, exists := clusters[configCluster.Name]; exists {
			result = append(result, tbnCluster)
		}
	}
	return result, nil
}
