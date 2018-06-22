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
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/strings"
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
	HealthCheck      *healthCheck      `json:"health_check"`
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

type healthCheck struct {
	TimeoutMsec        int       `json:"timeout_ms"`
	IntervalMsec       int       `json:"interval_ms"`
	UnhealthyThreshold int       `json:"unhealthy_threshold"`
	HealthyThreshold   int       `json:"healthy_threshold"`
	IntervalJitterMsec *int      `json:"interval_jitter_ms"`
	Type               string    `json:"type"`
	Path               string    `json:"path"`
	Send               []hexByte `json:"send"`
	Receive            []hexByte `json:"receive"`
	ServiceName        string    `json:"service_name"`
}

type hexByte struct {
	HexString string `json:"binary"`
}

type sds struct {
	Cluster cluster `json:"cluster"`
}

type cdsResp struct {
	Clusters []cluster `json:"clusters"`
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

func mkHealthChecks(hc *healthCheck) (api.HealthChecks, error) {
	if hc == nil {
		return nil, nil
	}

	hchr, err := mkHealthChecker(hc)
	if err != nil {
		return nil, err
	}

	return api.HealthChecks{
		{
			TimeoutMsec:        hc.TimeoutMsec,
			IntervalMsec:       hc.IntervalMsec,
			UnhealthyThreshold: hc.UnhealthyThreshold,
			HealthyThreshold:   hc.HealthyThreshold,
			IntervalJitterMsec: hc.IntervalJitterMsec,
			HealthChecker:      hchr,
		},
	}, nil
}

func mkHealthChecker(hc *healthCheck) (api.HealthChecker, error) {
	if hc == nil {
		return api.HealthChecker{}, nil
	}

	switch hc.Type {
	case "tcp":
		send, err := hexBytesToBase64String(hc.Send)
		if err != nil {
			return api.HealthChecker{}, fmt.Errorf(`Unable to parse "send": %s`, err)
		}

		receive, err := hexBytesToBase64String(hc.Receive)
		if err != nil {
			return api.HealthChecker{}, fmt.Errorf(`Unable to parse "receive": %s`, err)
		}

		var rSlice []string
		if receive != "" {
			rSlice = []string{receive}
		}

		return api.HealthChecker{
			TCPHealthCheck: &api.TCPHealthCheck{
				Send:    send,
				Receive: rSlice,
			},
		}, nil

	case "http":
		if hc.Path == "" {
			return api.HealthChecker{},
				errors.New("Path must be specified for http health checking")
		}

		return api.HealthChecker{
			HTTPHealthCheck: &api.HTTPHealthCheck{
				Path:        hc.Path,
				ServiceName: hc.ServiceName,
			},
		}, nil

	default:
		return api.HealthChecker{}, fmt.Errorf("Unsupported health check type: %s", hc.Type)
	}
}

// This implementation falls out of the way envoy maps the v1 to v2 version of
// this configuration:
// https://github.com/envoyproxy/envoy/blob/v1.7.0/source/common/config/cds_json.cc#L42
func hexBytesToBase64String(b []hexByte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}

	arr := []byte{}
	for _, bb := range b {
		bArr, err := hex.DecodeString(bb.HexString)
		if err != nil {
			return "", err
		}

		arr = append(arr, bArr...)
	}

	return base64.StdEncoding.EncodeToString(arr), nil
}
