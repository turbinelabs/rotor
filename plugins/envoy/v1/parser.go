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
	"github.com/turbinelabs/nonstdlib/strings"
	"github.com/turbinelabs/rotor/xds/collector"
)

type bootstrapConfig struct {
	ClusterManager clusterManager `json:"cluster_manager"`
}

type clusterManager struct {
	Clusters []cluster `json:"clusters"`
	Sds      sds       `json:"sds"`
}

type cluster struct {
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	ServiceName      string            `json:"service_name"`
	Hosts            []host            `json:"hosts"`
	CircuitBreakers  *circuitBreakers  `json:"circuit_breakers"`
	OutlierDetection *outlierDetection `json:"outlier_detection"`
}

type host struct {
	URL string `json:"url"`
}

type circuitBreakers struct {
	Default *perPriorityCircuitBreakers `json:"default"`
}

type perPriorityCircuitBreakers struct {
	MaxConnections     *int `json:"max_connections"`
	MaxPendingRequests *int `json:"max_pending_requests"`
	MaxRequests        *int `json:"max_requests"`
	MaxRetries         *int `json:"max_retries"`
}

type outlierDetection struct {
	Consecutive5xx                     *int `json:"consecutive_5xx"`
	ConsecutiveGatewayFailure          *int `json:"consecutive_gateway_failure"`
	IntervalMsec                       *int `json:"interval_ms"`
	BaseEjectionTimeMsec               *int `json:"base_ejection_time_ms"`
	MaxEjectionPercent                 *int `json:"max_ejection_percent"`
	EnforcingConsecutive5xx            *int `json:"enforcing_consecutive_5xx"`
	EnforcingConsecutiveGatewayFailure *int `json:"enforcing_consecutive_gateway_failure"`
	EnforcingSuccessRate               *int `json:"enforcing_success_rate"`
	SuccessRateMinimumHosts            *int `json:"success_rate_minimum_hosts"`
	SuccessRateRequestVolume           *int `json:"success_rate_request_volume"`
	SuccessRateStdevFactor             *int `json:"success_rate_stdev_factor"`
}

type sds struct {
	Cluster cluster `json:"cluster"`
}

type cdsResp struct {
	Clusters []cluster `json:"clusters"`
}

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

		cluster := api.Cluster{
			Name:             c.Name,
			Instances:        instances,
			CircuitBreakers:  mkCircuitBreakers(c.CircuitBreakers),
			OutlierDetection: mkOutlierDetection(c.OutlierDetection),
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

func mkInstance(envoyHostPort string) (api.Instance, error) {
	protocol, hostAndPort := strings.Split2(envoyHostPort, "://")

	switch protocol {
	case "udp":
		return api.Instance{}, fmt.Errorf("UDP not supported: %s", envoyHostPort)

	case "tcp":
		host, port, err := strings.SplitHostPort(hostAndPort)
		if err != nil {
			return api.Instance{}, fmt.Errorf("Error parsing %s: %s", envoyHostPort, err)
		}

		return api.Instance{Host: host, Port: port}, nil

	default:
		return api.Instance{}, fmt.Errorf("Unrecognized protocol: %s", envoyHostPort)
	}
}

func mkCircuitBreakers(v1Cbs *circuitBreakers) *api.CircuitBreakers {
	if v1Cbs == nil || v1Cbs.Default == nil {
		return nil
	}

	return &api.CircuitBreakers{
		MaxConnections:     v1Cbs.Default.MaxConnections,
		MaxPendingRequests: v1Cbs.Default.MaxPendingRequests,
		MaxRetries:         v1Cbs.Default.MaxRetries,
		MaxRequests:        v1Cbs.Default.MaxRequests,
	}
}

func mkOutlierDetection(od *outlierDetection) *api.OutlierDetection {
	if od == nil {
		return nil
	}

	return &api.OutlierDetection{
		IntervalMsec:                       od.IntervalMsec,
		BaseEjectionTimeMsec:               od.BaseEjectionTimeMsec,
		MaxEjectionPercent:                 od.MaxEjectionPercent,
		Consecutive5xx:                     od.Consecutive5xx,
		EnforcingConsecutive5xx:            od.EnforcingConsecutive5xx,
		EnforcingSuccessRate:               od.EnforcingSuccessRate,
		SuccessRateMinimumHosts:            od.SuccessRateMinimumHosts,
		SuccessRateRequestVolume:           od.SuccessRateRequestVolume,
		SuccessRateStdevFactor:             od.SuccessRateStdevFactor,
		ConsecutiveGatewayFailure:          od.ConsecutiveGatewayFailure,
		EnforcingConsecutiveGatewayFailure: od.EnforcingConsecutiveGatewayFailure,
	}
}
