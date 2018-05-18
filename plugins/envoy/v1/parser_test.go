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
	"bytes"
	"errors"
	"testing"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor/xds/collector"
	"github.com/turbinelabs/test/assert"
)

const (
	jsonClustersNoSds = `
  "clusters": [
      {
        "name": "statsd",
        "type": "static",
        "hosts": [
          {
            "url": "tcp://127.0.0.1:8125"
          }
        ]
      },
      {
        "name": "backhaul",
        "type": "strict_dns",
        "hosts": [
          {
            "url": "tcp://front-proxy.yourcompany.net:9400"
          }
        ],
        "circuit_breakers": {
          "high": {
            "max_connections": 100,
            "max_pending_requests": 200,
            "max_requests": 300,
            "max_retries": 400
          },
          "default": {
            "max_connections": 1,
            "max_pending_requests": 2,
            "max_requests": 3,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "consecutive_5xx": 100,
          "interval_ms": 100,
          "enforcing_consecutive_5xx": 100,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 0
        }
      },
      {
        "name": "lightstep_saas",
        "type": "logical_dns",
        "hosts": [
          {
            "url": "tcp://collector-grpc.lightstep.com:443"
          }
        ],
        "circuit_breakers": {
          "default": {
            "max_pending_requests": 1,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "interval_ms": 30000,
          "enforcing_consecutive_5xx": 0,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 100
        }
      }
    ]
`
	jsonCdsResponse = "{" + jsonClustersNoSds + "}"

	jsonInputNoSdsClusterSdsDefined = `
{
  "cluster_manager": {` + jsonClustersNoSds + `,
    "sds": {
      "cluster": {
        "name": "sds",
        "connect_timeout_ms": 250,
        "type": "strict_dns",
        "lb_type": "round_robin",
        "hosts": [
          {
            "url": "tcp://127.0.0.1:8080"
          }
        ]
      },
      "refresh_delay_ms": 30000
    }
  }
}
`
	jsonClustersWithSds = `
    "clusters": [
      {
        "name": "statsd",
        "type": "static",
        "hosts": [
          {
            "url": "tcp://127.0.0.1:8125"
          }
        ]
      },
      {
        "name": "backhaul",
        "type": "strict_dns",
        "hosts": [
          {
            "url": "tcp://front-proxy.yourcompany.net:9400"
          }
        ],
        "circuit_breakers": {
          "default": {
            "max_connections": 1,
            "max_pending_requests": 2,
            "max_requests": 3,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "consecutive_5xx": 100,
          "interval_ms": 100,
          "enforcing_consecutive_5xx": 100,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 0
        }
      },
      {
        "name": "lightstep_saas",
        "type": "logical_dns",
        "hosts": [
          {
            "url": "tcp://collector-grpc.lightstep.com:443"
          }
        ],
        "circuit_breakers": {
          "default": {
            "max_pending_requests": 1,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "interval_ms": 30000,
          "enforcing_consecutive_5xx": 0,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 100
        }
      },
      {
        "name": "sds_cluster",
        "type": "sds"
      }
    ]
`
	jsonCdsResponseWithSds = "{" + jsonClustersWithSds + "}"

	jsonInputSdsClusterSdsDefined = `
{
  "cluster_manager": {` + jsonClustersWithSds + `,
    "sds": {
      "cluster": {
        "name": "sds",
        "connect_timeout_ms": 250,
        "type": "strict_dns",
        "lb_type": "round_robin",
        "hosts": [
          {
            "url": "tcp://127.0.0.1:8080"
          }
        ]
      },
      "refresh_delay_ms": 30000
    }
  }
}
`
	jsonInputSdsClusterNoSdsDefined = `
{
  "cluster_manager": {
    "clusters": [
      {
        "name": "statsd",
        "type": "static",
        "hosts": [
          {
            "url": "tcp://127.0.0.1:8125"
          }
        ]
      },
      {
        "name": "sds_cluster",
        "type": "sds"
      },
      {
        "name": "backhaul",
        "type": "strict_dns",
        "hosts": [
          {
            "url": "tcp://front-proxy.yourcompany.net:9400"
          }
        ],
        "circuit_breakers": {
          "default": {
            "max_connections": 1,
            "max_pending_requests": 2,
            "max_requests": 3,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "consecutive_5xx": 100,
          "interval_ms": 100,
          "enforcing_consecutive_5xx": 100,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 0
        }
      },
      {
        "name": "lightstep_saas",
        "type": "logical_dns",
        "hosts": [
          {
            "url": "tcp://collector-grpc.lightstep.com:443"
          }
        ],
        "circuit_breakers": {
          "default": {
            "max_pending_requests": 1,
            "max_retries": 4
          }
        },
        "outlier_detection": {
          "interval_ms": 30000,
          "enforcing_consecutive_5xx": 0,
          "enforcing_consecutive_gateway_failure": 0,
          "enforcing_success_rate": 100
        }
      }
    ]
  }
}
`

	YamlInput = `
cluster_manager:
  clusters:
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
  - name: backhaul
    type: strict_dns
    hosts:
    - url: tcp://front-proxy.yourcompany.net:9400
    circuit_breakers:
      default:
        max_connections: 1
        max_pending_requests: 2
        max_requests: 3
        max_retries: 4
    outlier_detection:
      interval_ms: 100
      consecutive_5xx: 100
      enforcing_consecutive_5xx: 100
      enforcing_consecutive_gateway_failure: 0
      enforcing_success_rate: 0
  - name: lightstep_saas
    type: logical_dns
    hosts:
    - url: tcp://collector-grpc.lightstep.com:443
    circuit_breakers:
      default:
        max_pending_requests: 1
        max_retries: 4
    outlier_detection:
      interval_ms: 30000
      enforcing_consecutive_5xx: 0
      enforcing_consecutive_gateway_failure: 0
      enforcing_success_rate: 100
`

	YamlInputDuplicateClusters = `
cluster_manager:
  clusters:
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
  - name: backhaul
    type: strict_dns
    hosts:
    - url: tcp://front-proxy.yourcompany.net:9400
  - name: lightstep_saas
    type: logical_dns
    hosts:
    - url: tcp://collector-grpc.lightstep.com:443
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
`

	YamlInputUDPInstance = `
cluster_manager:
  clusters:
  - name: statsd
    type: static
    hosts:
    - url: udp://127.0.0.1:8125
  - name: backhaul
    type: strict_dns
    hosts:
    - url: tcp://front-proxy.yourcompany.net:9400
  - name: lightstep_saas
    type: logical_dns
    hosts:
    - url: tcp://collector-grpc.lightstep.com:443
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
`

	YamlInputBadProtocolInstance = `
cluster_manager:
  clusters:
  - name: statsd
    type: static
    hosts:
    - url: blarg://127.0.0.1:8125
  - name: backhaul
    type: strict_dns
    hosts:
    - url: tcp://front-proxy.yourcompany.net:9400
  - name: lightstep_saas
    type: logical_dns
    hosts:
    - url: tcp://collector-grpc.lightstep.com:443
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
`

	YamlInputBadHostPortInput = `
cluster_manager:
  clusters:
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
  - name: backhaul
    type: strict_dns
    hosts:
    - url: tcp://front-proxy.yourcompany.net
  - name: lightstep_saas
    type: logical_dns
    hosts:
    - url: tcp://collector-grpc.lightstep.com:443
  - name: statsd
    type: static
    hosts:
    - url: tcp://127.0.0.1:8125
`
)

var (
	expectedClusters = api.Clusters{
		{
			Name: "statsd",
			Instances: api.Instances{{
				Host: "127.0.0.1",
				Port: 8125,
			}},
		},
		{
			Name: "backhaul",
			Instances: api.Instances{{
				Host: "front-proxy.yourcompany.net",
				Port: 9400,
			}},
			CircuitBreakers: &api.CircuitBreakers{
				MaxConnections:     ptr.Int(1),
				MaxPendingRequests: ptr.Int(2),
				MaxRequests:        ptr.Int(3),
				MaxRetries:         ptr.Int(4),
			},
			OutlierDetection: &api.OutlierDetection{
				Consecutive5xx:                     ptr.Int(100),
				IntervalMsec:                       ptr.Int(100),
				EnforcingConsecutive5xx:            ptr.Int(100),
				EnforcingConsecutiveGatewayFailure: ptr.Int(0),
				EnforcingSuccessRate:               ptr.Int(0),
			},
		},
		{
			Name: "lightstep_saas",
			Instances: api.Instances{{
				Host: "collector-grpc.lightstep.com",
				Port: 443,
			}},
			CircuitBreakers: &api.CircuitBreakers{
				MaxPendingRequests: ptr.Int(1),
				MaxRetries:         ptr.Int(4),
			},
			OutlierDetection: &api.OutlierDetection{
				IntervalMsec:                       ptr.Int(30000),
				EnforcingConsecutive5xx:            ptr.Int(0),
				EnforcingConsecutiveGatewayFailure: ptr.Int(0),
				EnforcingSuccessRate:               ptr.Int(100),
			},
		},
	}
)

type MockResolverFactory struct {
	callCnt     int
	resolver    collector.ClusterResolver
	errToReturn error
}

func (mrf *MockResolverFactory) do(clusterManager) (collector.ClusterResolver, error) {
	mrf.callCnt++
	return mrf.resolver, mrf.errToReturn
}

func TestFileParserGoodJSON(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewJson(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(jsonInputNoSdsClusterSdsDefined))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserGoodYaml(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(YamlInput))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserJSONAndYamlFunctionallyEquivalent(t *testing.T) {
	mrf := &MockResolverFactory{}
	yamlParser := newFileParser(codec.NewYaml(), mrf.do)
	jsonParser := newFileParser(codec.NewJson(), mrf.do)

	yamlC, yamlE := yamlParser.parse(bytes.NewBufferString(YamlInput))
	assert.Nil(t, yamlE)

	jsonC, jsonE := jsonParser.parse(bytes.NewBufferString(jsonInputNoSdsClusterSdsDefined))
	assert.Nil(t, jsonE)

	assert.HasSameElements(t, yamlC, jsonC)
	assert.Equal(t, mrf.callCnt, 2)
}

func TestFileParserCodecError(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString("blerpy blerp"))
	assert.NonNil(t, err)
	assert.Nil(t, clusters)
	assert.Equal(t, mrf.callCnt, 0)
}

func TestParserSdsClusterCreationReturnsError(t *testing.T) {
	mrf := &MockResolverFactory{
		errToReturn: errors.New("boom"),
	}
	p := newFileParser(codec.NewJson(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(jsonInputSdsClusterNoSdsDefined))
	assert.NonNil(t, err)
	assert.Nil(t, clusters)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserResolverFactoryReturnsNilResolver(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewJson(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(jsonInputSdsClusterNoSdsDefined))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserDuplicateClustersError(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(YamlInputDuplicateClusters))
	assert.NonNil(t, err)
	assert.Nil(t, clusters)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserUDPProtocolHostError(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	cs, err := p.parse(bytes.NewBufferString(YamlInputUDPInstance))
	assert.Nil(t, cs)
	assert.NonNil(t, err)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserUnknownProtocolHostError(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	cs, err := p.parse(bytes.NewBufferString(YamlInputBadProtocolInstance))
	assert.Nil(t, cs)
	assert.NonNil(t, err)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserBadHostPort(t *testing.T) {
	mrf := &MockResolverFactory{}
	p := newFileParser(codec.NewYaml(), mrf.do)

	cs, err := p.parse(bytes.NewBufferString(YamlInputBadHostPortInput))
	assert.Nil(t, cs)
	assert.NonNil(t, err)
	assert.Equal(t, mrf.callCnt, 1)
}

func TestFileParserClusterResolverErrorsIgnored(t *testing.T) {
	resolverCallCnt := 0
	mrf := &MockResolverFactory{
		resolver: func(string) (api.Instances, []error) {
			resolverCallCnt++
			return nil, []error{errors.New("no bueno")}
		},
	}

	p := newFileParser(codec.NewJson(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(jsonInputSdsClusterNoSdsDefined))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, mrf.callCnt, 1)
	assert.Equal(t, resolverCallCnt, 1)
}

func TestFileParserGoodJsonWithSds(t *testing.T) {
	sdsInstances := api.Instances{
		{
			Host: "myhost.com",
			Port: 9000,
		},
		{
			Host: "myhost2.com",
			Port: 9000,
		},
	}

	sdsCluster := api.Cluster{
		Name:      "sds_cluster",
		Instances: sdsInstances,
	}

	resolverCallCnt := 0
	mrf := &MockResolverFactory{
		resolver: func(string) (api.Instances, []error) {
			resolverCallCnt++
			return sdsInstances, nil
		},
	}

	p := newFileParser(codec.NewJson(), mrf.do)

	clusters, err := p.parse(bytes.NewBufferString(jsonInputSdsClusterSdsDefined))
	assert.Nil(t, err)

	expectedClusters := append(expectedClusters, sdsCluster)
	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, mrf.callCnt, 1)
	assert.Equal(t, resolverCallCnt, 1)
}

func TestCdsParserGoodJsonNoSds(t *testing.T) {
	resolverCallCnt := 0
	p := newCdsParser(func(string) (api.Instances, []error) {
		resolverCallCnt++
		return nil, nil
	})

	clusters, err := p.parse(bytes.NewBufferString(jsonCdsResponse))
	assert.Nil(t, err)

	assert.HasSameElements(t, clusters, expectedClusters)
	assert.Equal(t, resolverCallCnt, 0)
}

func TestCdsParserGoodJsonWithSds(t *testing.T) {
	sdsInstances := api.Instances{
		{
			Host: "myhost.com",
			Port: 9000,
		},
		{
			Host: "myhost2.com",
			Port: 9000,
		},
	}

	sdsCluster := api.Cluster{
		Name:      "sds_cluster",
		Instances: sdsInstances,
	}

	resolverCallCnt := 0
	p := newCdsParser(func(string) (api.Instances, []error) {
		resolverCallCnt++
		return sdsInstances, nil
	})

	clusters, err := p.parse(bytes.NewBufferString(jsonCdsResponseWithSds))
	assert.Nil(t, err)

	assert.HasSameElements(t, clusters, append(expectedClusters, sdsCluster))
	assert.Equal(t, resolverCallCnt, 1)
}

func TestCdsParserDecodeError(t *testing.T) {
	resolverCallCnt := 0
	p := newCdsParser(func(string) (api.Instances, []error) {
		resolverCallCnt++
		return nil, nil
	})

	clusters, err := p.parse(bytes.NewBufferString("nope"))
	assert.Nil(t, clusters)
	assert.NonNil(t, err)
	assert.Equal(t, resolverCallCnt, 0)
}
