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
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
)

const (
	clusterConnectTimeoutSecs = 10
)

type cds struct {
	caFile   string
	template *envoyapi.Cluster
}

// adapt turns poller.Objects into Cluster cache.Resources
func (s cds) adapt(objects *poller.Objects) (cache.Resources, error) {
	resources := make(map[string]cache.Resource, len(objects.Clusters))
	for _, cluster := range objects.Clusters {
		envoyCluster, err := s.tbnToEnvoyCluster(cluster, objects)
		if err != nil {
			return cache.Resources{}, err
		}
		resources[envoyCluster.GetName()] = envoyCluster
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
}

func (s cds) withTemplate(l *envoyapi.Cluster) clusterAdapter {
	if l != nil {
		console.Debug().Println("using cluster template")
	}
	return cds{
		caFile:   s.caFile,
		template: l,
	}
}

func (s cds) tbnToEnvoyCluster(
	tbnCluster tbnapi.Cluster,
	objects *poller.Objects,
) (*envoyapi.Cluster, error) {
	if s.template != nil {
		// force copy
		template := *s.template
		c := &template
		c.Name = tbnCluster.Name
		if c.GetEdsClusterConfig() != nil {
			// force copy
			config := *c.EdsClusterConfig
			c.EdsClusterConfig = &config
			c.EdsClusterConfig.ServiceName = tbnCluster.Name
		}
		return c, nil
	}

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
			tlsContext.CommonTlsContext.ValidationContextType = &envoyauth.CommonTlsContext_ValidationContext{
				ValidationContext: &envoyauth.CertificateValidationContext{
					TrustedCa: &envoycore.DataSource{
						Specifier: &envoycore.DataSource_Filename{
							Filename: s.caFile,
						},
					},
				},
			}
		}
	}

	ehcs, err := tbnToEnvoyHealthChecks(tbnCluster.HealthChecks)
	if err != nil {
		return nil, err
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
		HealthChecks:     ehcs,
	}, nil
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

func tbnToEnvoyHealthChecks(thcs tbnapi.HealthChecks) ([]*envoycore.HealthCheck, error) {
	if len(thcs) == 0 {
		return nil, nil
	}

	ehcs := make([]*envoycore.HealthCheck, len(thcs))
	for i, thc := range thcs {
		ehc, err := tbnToEnvoyHealthCheck(thc)
		if err != nil {
			return nil, err
		}

		ehcs[i] = ehc
	}

	return ehcs, nil
}

func tbnToEnvoyHealthCheck(thc tbnapi.HealthCheck) (*envoycore.HealthCheck, error) {
	o := &envoycore.HealthCheck{
		Timeout:               intToTimeDurationPtr(thc.TimeoutMsec, time.Millisecond),
		Interval:              intToTimeDurationPtr(thc.IntervalMsec, time.Millisecond),
		IntervalJitter:        intPtrToDurationPtr(thc.IntervalJitterMsec),
		UnhealthyThreshold:    intToUint32Ptr(thc.UnhealthyThreshold),
		HealthyThreshold:      intToUint32Ptr(thc.HealthyThreshold),
		ReuseConnection:       boolPtrToBoolValue(thc.ReuseConnection),
		NoTrafficInterval:     intPtrToDurationPtr(thc.NoTrafficIntervalMsec),
		UnhealthyInterval:     intPtrToDurationPtr(thc.UnhealthyIntervalMsec),
		UnhealthyEdgeInterval: intPtrToDurationPtr(thc.UnhealthyEdgeIntervalMsec),
		HealthyEdgeInterval:   intPtrToDurationPtr(thc.HealthyEdgeIntervalMsec),
	}

	switch {
	case thc.HealthChecker.HTTPHealthCheck != nil:
		o.HealthChecker = tbnToEnvoyHTTPHealthCheck(thc.HealthChecker.HTTPHealthCheck)

	case thc.HealthChecker.TCPHealthCheck != nil:
		ethc, err := tbnToEnvoyTCPHealthCheck(thc.HealthChecker.TCPHealthCheck)
		if err != nil {
			return nil, err
		}
		o.HealthChecker = ethc
	}

	return o, nil
}

func tbnToEnvoyHTTPHealthCheck(hhc *tbnapi.HTTPHealthCheck) *envoycore.HealthCheck_HttpHealthCheck_ {
	var hvo []*envoycore.HeaderValueOption
	if len(hhc.RequestHeadersToAdd) > 0 {
		hvo = make([]*envoycore.HeaderValueOption, len(hhc.RequestHeadersToAdd))
		for i, v := range hhc.RequestHeadersToAdd {
			hvo[i] = &envoycore.HeaderValueOption{
				Header: &envoycore.HeaderValue{
					Key:   v.Key,
					Value: v.Value,
				},
			}
		}
	}
	return &envoycore.HealthCheck_HttpHealthCheck_{
		HttpHealthCheck: &envoycore.HealthCheck_HttpHealthCheck{
			Host:                hhc.Host,
			Path:                hhc.Path,
			ServiceName:         hhc.ServiceName,
			RequestHeadersToAdd: hvo,
		},
	}
}

func tbnToEnvoyTCPHealthCheck(
	tthc *tbnapi.TCPHealthCheck,
) (*envoycore.HealthCheck_TcpHealthCheck_, error) {
	if tthc == nil {
		return nil, nil
	}

	sendPayload, err := base64StringToPayload(tthc.Send)
	if err != nil {
		return nil, err
	}

	var receivePayloads []*envoycore.HealthCheck_Payload
	if len(tthc.Receive) > 0 {
		receivePayloads = make([]*envoycore.HealthCheck_Payload, len(tthc.Receive))

		for i, bStr := range tthc.Receive {
			p, err := base64StringToPayload(bStr)
			if err != nil {
				return nil, err
			}
			receivePayloads[i] = p
		}
	}

	return &envoycore.HealthCheck_TcpHealthCheck_{
		TcpHealthCheck: &envoycore.HealthCheck_TcpHealthCheck{
			Send:    sendPayload,
			Receive: receivePayloads,
		},
	}, nil
}

func envoyToTbnHealthChecks(ehcs []*envoycore.HealthCheck) tbnapi.HealthChecks {
	if len(ehcs) == 0 {
		return nil
	}

	thcs := make(tbnapi.HealthChecks, len(ehcs))
	for i, ehc := range ehcs {
		thcs[i] = envoyToTbnHealthCheck(ehc)
	}

	return thcs
}

func envoyToTbnHealthCheck(ehc *envoycore.HealthCheck) tbnapi.HealthCheck {
	if ehc == nil {
		return tbnapi.HealthCheck{}
	}

	o := tbnapi.HealthCheck{
		TimeoutMsec:               timeDurationPtrToInt(ehc.Timeout, time.Millisecond),
		IntervalMsec:              timeDurationPtrToInt(ehc.Interval, time.Millisecond),
		IntervalJitterMsec:        durationPtrToIntPtr(ehc.IntervalJitter),
		UnhealthyThreshold:        uint32PtrToInt(ehc.UnhealthyThreshold),
		HealthyThreshold:          uint32PtrToInt(ehc.HealthyThreshold),
		ReuseConnection:           boolValueToBoolPtr(ehc.ReuseConnection),
		NoTrafficIntervalMsec:     durationPtrToIntPtr(ehc.NoTrafficInterval),
		UnhealthyIntervalMsec:     durationPtrToIntPtr(ehc.UnhealthyInterval),
		UnhealthyEdgeIntervalMsec: durationPtrToIntPtr(ehc.UnhealthyEdgeInterval),
		HealthyEdgeIntervalMsec:   durationPtrToIntPtr(ehc.HealthyEdgeInterval),
	}

	if ehhc := ehc.GetHttpHealthCheck(); ehhc != nil {
		o.HealthChecker.HTTPHealthCheck = envoyToTbnHTTPHealthCheck(ehhc)
	}

	if ethc := ehc.GetTcpHealthCheck(); ethc != nil {
		o.HealthChecker.TCPHealthCheck = envoyToTbnTCPHealthCheck(ethc)
	}

	return o
}

func envoyToTbnHTTPHealthCheck(ehhc *envoycore.HealthCheck_HttpHealthCheck) *tbnapi.HTTPHealthCheck {
	if ehhc == nil {
		return nil
	}

	var hta tbnapi.Metadata
	if len(ehhc.GetRequestHeadersToAdd()) > 0 {
		hta = make(tbnapi.Metadata, len(ehhc.GetRequestHeadersToAdd()))
		for i, hvo := range ehhc.GetRequestHeadersToAdd() {
			if h := hvo.GetHeader(); h != nil {
				hta[i] = tbnapi.Metadatum{Key: h.GetKey(), Value: h.GetValue()}
			}
		}
	}

	return &tbnapi.HTTPHealthCheck{
		Path:                ehhc.GetPath(),
		Host:                ehhc.GetHost(),
		ServiceName:         ehhc.GetServiceName(),
		RequestHeadersToAdd: hta,
	}
}

func envoyToTbnTCPHealthCheck(ethc *envoycore.HealthCheck_TcpHealthCheck) *tbnapi.TCPHealthCheck {
	if ethc == nil {
		return nil
	}

	var sendStr string
	if shp := ethc.GetSend(); len(shp.GetBinary()) > 0 {
		sendStr = bytesToBase64String(shp.GetBinary())
	}

	var receiveStrs []string
	if len(ethc.GetReceive()) > 0 {
		receiveStrs = make([]string, len(ethc.GetReceive()))
		for i, p := range ethc.GetReceive() {
			if len(p.GetBinary()) > 0 {
				receiveStrs[i] = bytesToBase64String(p.GetBinary())
			}
		}
	}

	return &tbnapi.TCPHealthCheck{
		Send:    sendStr,
		Receive: receiveStrs,
	}
}
