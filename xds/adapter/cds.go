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
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoycluster "github.com/envoyproxy/go-control-plane/envoy/api/v2/cluster"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/poller"
)

const (
	clusterConnectTimeoutSecs = 10
)

type cds struct {
	caFile string
}

// resourceAdapter turns poller.Objects into Cluster cache.Resources
func (s cds) resourceAdapter(objects *poller.Objects) (cache.Resources, error) {
	resources := make(map[string]cache.Resource, len(objects.Clusters))
	for _, cluster := range objects.Clusters {
		envoyCluster := s.tbnToEnvoyCluster(cluster, objects)
		resources[envoyCluster.GetName()] = s.tbnToEnvoyCluster(cluster, objects)
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
}

func (s cds) tbnToEnvoyCluster(
	tbnCluster tbnapi.Cluster,
	objects *poller.Objects,
) *envoyapi.Cluster {
	subsets := objects.SubsetsPerCluster(tbnCluster.ClusterKey)

	var subsetConfig *envoyapi.Cluster_LbSubsetConfig

	if subsets != nil {
		subsetSelectors :=
			make([]*envoyapi.Cluster_LbSubsetConfig_LbSubsetSelector, 0, len(subsets))

		for _, subset := range subsets {
			subsetSelectors = append(
				subsetSelectors,
				&envoyapi.Cluster_LbSubsetConfig_LbSubsetSelector{Keys: subset},
			)
		}
		subsetConfig = &envoyapi.Cluster_LbSubsetConfig{
			FallbackPolicy:  envoyapi.Cluster_LbSubsetConfig_ANY_ENDPOINT,
			SubsetSelectors: subsetSelectors,
		}
	}

	var tlsContext *envoyauth.UpstreamTlsContext
	if tbnCluster.RequireTLS {
		tlsContext = &envoyauth.UpstreamTlsContext{
			CommonTlsContext: &envoyauth.CommonTlsContext{
				TlsParams: &envoyauth.TlsParameters{
					TlsMinimumProtocolVersion: envoyauth.TlsParameters_TLS_AUTO,
					TlsMaximumProtocolVersion: envoyauth.TlsParameters_TLS_AUTO,
				},
			},
			Sni: tbnCluster.Name,
		}

		if s.caFile != "" {
			tlsContext.CommonTlsContext.ValidationContext = &envoyauth.CertificateValidationContext{
				TrustedCa: &envoycore.DataSource{
					Specifier: &envoycore.DataSource_Filename{
						Filename: s.caFile,
					},
				},
			}
		}
	}

	return &envoyapi.Cluster{
		Name: tbnCluster.Name,
		Type: envoyapi.Cluster_EDS,
		EdsClusterConfig: &envoyapi.Cluster_EdsClusterConfig{
			EdsConfig:   &xdsClusterConfig,
			ServiceName: tbnCluster.Name,
		},
		ConnectTimeout:   clusterConnectTimeoutSecs * time.Second,
		LbPolicy:         envoyapi.Cluster_LEAST_REQUEST,
		TlsContext:       tlsContext,
		LbSubsetConfig:   subsetConfig,
		CircuitBreakers:  tbnToEnvoyCircuitBreakers(tbnCluster.CircuitBreakers),
		OutlierDetection: tbnToEnvoyOutlierDetection(tbnCluster.OutlierDetection),
	}
}

func tbnToEnvoyCircuitBreakers(tbnCb *tbnapi.CircuitBreakers) *envoycluster.CircuitBreakers {
	if tbnCb == nil {
		return nil
	}

	return &envoycluster.CircuitBreakers{
		Thresholds: []*envoycluster.CircuitBreakers_Thresholds{
			{
				Priority:           defaultRoutingPriority,
				MaxConnections:     intPtrToUint32Ptr(tbnCb.MaxConnections),
				MaxPendingRequests: intPtrToUint32Ptr(tbnCb.MaxPendingRequests),
				MaxRetries:         intPtrToUint32Ptr(tbnCb.MaxRetries),
				MaxRequests:        intPtrToUint32Ptr(tbnCb.MaxRequests),
			},
		},
	}
}

func envoyToTbnCircuitBreakers(ecb *envoycluster.CircuitBreakers) *tbnapi.CircuitBreakers {
	if ecb == nil {
		return nil
	}

	for _, cbt := range ecb.GetThresholds() {
		if cbt.GetPriority() == envoycore.RoutingPriority_DEFAULT {
			return &tbnapi.CircuitBreakers{
				MaxConnections:     uint32PtrToIntPtr(cbt.GetMaxConnections()),
				MaxPendingRequests: uint32PtrToIntPtr(cbt.GetMaxPendingRequests()),
				MaxRequests:        uint32PtrToIntPtr(cbt.GetMaxRequests()),
				MaxRetries:         uint32PtrToIntPtr(cbt.GetMaxRetries()),
			}
		}
	}

	return nil
}

func tbnToEnvoyOutlierDetection(tod *tbnapi.OutlierDetection) *envoycluster.OutlierDetection {
	if tod == nil {
		return nil
	}

	return &envoycluster.OutlierDetection{
		Consecutive_5Xx:                    intPtrToUint32Ptr(tod.Consecutive5xx),
		Interval:                           intPtrToDurationPtr(tod.IntervalMsec),
		BaseEjectionTime:                   intPtrToDurationPtr(tod.BaseEjectionTimeMsec),
		MaxEjectionPercent:                 intPtrToUint32Ptr(tod.MaxEjectionPercent),
		EnforcingConsecutive_5Xx:           intPtrToUint32Ptr(tod.EnforcingConsecutive5xx),
		EnforcingSuccessRate:               intPtrToUint32Ptr(tod.EnforcingSuccessRate),
		SuccessRateMinimumHosts:            intPtrToUint32Ptr(tod.SuccessRateMinimumHosts),
		SuccessRateRequestVolume:           intPtrToUint32Ptr(tod.SuccessRateRequestVolume),
		SuccessRateStdevFactor:             intPtrToUint32Ptr(tod.SuccessRateStdevFactor),
		ConsecutiveGatewayFailure:          intPtrToUint32Ptr(tod.ConsecutiveGatewayFailure),
		EnforcingConsecutiveGatewayFailure: intPtrToUint32Ptr(tod.EnforcingConsecutiveGatewayFailure),
	}
}

func envoyToTbnOutlierDetection(eod *envoycluster.OutlierDetection) *tbnapi.OutlierDetection {
	if eod == nil {
		return nil
	}

	return &tbnapi.OutlierDetection{
		IntervalMsec:                       durationPtrToIntPtr(eod.GetInterval()),
		BaseEjectionTimeMsec:               durationPtrToIntPtr(eod.GetBaseEjectionTime()),
		MaxEjectionPercent:                 uint32PtrToIntPtr(eod.GetMaxEjectionPercent()),
		Consecutive5xx:                     uint32PtrToIntPtr(eod.GetConsecutive_5Xx()),
		EnforcingConsecutive5xx:            uint32PtrToIntPtr(eod.GetEnforcingConsecutive_5Xx()),
		EnforcingSuccessRate:               uint32PtrToIntPtr(eod.GetEnforcingSuccessRate()),
		SuccessRateMinimumHosts:            uint32PtrToIntPtr(eod.GetSuccessRateMinimumHosts()),
		SuccessRateRequestVolume:           uint32PtrToIntPtr(eod.GetSuccessRateRequestVolume()),
		SuccessRateStdevFactor:             uint32PtrToIntPtr(eod.GetSuccessRateStdevFactor()),
		ConsecutiveGatewayFailure:          uint32PtrToIntPtr(eod.GetConsecutiveGatewayFailure()),
		EnforcingConsecutiveGatewayFailure: uint32PtrToIntPtr(eod.GetEnforcingConsecutiveGatewayFailure()),
	}
}
