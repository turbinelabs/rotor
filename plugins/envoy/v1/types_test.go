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
	"testing"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/test/assert"
)

func TestMkHealthChecksNilInput(t *testing.T) {
	h, e := mkHealthChecks(nil)
	assert.Nil(t, h)
	assert.Nil(t, e)
}

func TestMkHealthChecksReturnErrForInvalidType(t *testing.T) {
	h, e := mkHealthChecks(&healthCheck{})
	assert.Nil(t, h)
	assert.ErrorContains(t, e, "Unsupported health check type")
}

func TestMkHealthChecksFailsOnBadTCPSend(t *testing.T) {
	h, e := mkHealthChecks(
		&healthCheck{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			Type:               "tcp",
			Send: []hexByte{
				{
					HexString: "ZZZ",
				},
			},
			Receive: []hexByte{
				{
					HexString: "EE",
				},
				{
					HexString: "11",
				},
			},
		},
	)

	assert.Nil(t, h)
	assert.ErrorContains(t, e, `Unable to parse "send"`)
}

func TestMkHealthChecksFailsOnBadTCPReceive(t *testing.T) {
	h, e := mkHealthChecks(
		&healthCheck{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			Type:               "tcp",
			Send: []hexByte{
				{
					HexString: "AA",
				},
			},
			Receive: []hexByte{
				{
					HexString: "BEEP",
				},
			},
		},
	)

	assert.Nil(t, h)
	assert.ErrorContains(t, e, `Unable to parse "receive"`)
}

func TestMkHealthChecksReturnsTCPCheckerSuccessfully(t *testing.T) {
	h, e := mkHealthChecks(
		&healthCheck{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			Type:               "tcp",
			Send: []hexByte{
				{
					HexString: "39000000",
				},
				{
					HexString: "24636d6400",
				},
				{
					HexString: "000000000000f03f",
				},
			},
			Receive: []hexByte{
				{
					HexString: "EE",
				},
				{
					HexString: "11",
				},
			},
		},
	)

	assert.Nil(t, e)
	assert.DeepEqual(t, h, api.HealthChecks{
		{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			HealthChecker: api.HealthChecker{
				TCPHealthCheck: &api.TCPHealthCheck{
					Send:    "OQAAACRjbWQAAAAAAAAA8D8=",
					Receive: []string{"7hE="},
				},
			},
		},
	})
}

func TestMkHealthChecksFailsOnEmptyHTTPPath(t *testing.T) {
	h, e := mkHealthChecks(
		&healthCheck{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			Type:               "http",
			ServiceName:        "health-check-service",
		},
	)

	assert.Nil(t, h)
	assert.ErrorContains(t, e, "Path must be specified for http health checking")
}

func TestMkHealthChecksReturnsHTTPCheckerSuccessfully(t *testing.T) {
	h, e := mkHealthChecks(
		&healthCheck{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			Type:               "http",
			Path:               "/some/path",
			ServiceName:        "health-check-service",
		},
	)

	assert.Nil(t, e)
	assert.DeepEqual(t, h, api.HealthChecks{
		{
			TimeoutMsec:        1,
			IntervalMsec:       2,
			UnhealthyThreshold: 3,
			HealthyThreshold:   4,
			IntervalJitterMsec: ptr.Int(5),
			HealthChecker: api.HealthChecker{
				HTTPHealthCheck: &api.HTTPHealthCheck{
					Path:        "/some/path",
					ServiceName: "health-check-service",
				},
			},
		},
	})
}

func TestHexBytesToBase64StringNilInput(t *testing.T) {
	s, e := hexBytesToBase64String(nil)
	assert.Equal(t, s, "")
	assert.Nil(t, e)
}

func TestHexBytesToBase64StringInvalidCharacterCount(t *testing.T) {
	s, e := hexBytesToBase64String([]hexByte{{HexString: "a"}})
	assert.Equal(t, s, "")
	assert.ErrorContains(t, e, "encoding/hex: odd length hex string")
}

func TestHexBytesToBase64StringPassesThroughDecodingError(t *testing.T) {
	s, e := hexBytesToBase64String([]hexByte{{HexString: "zz"}})
	assert.Equal(t, s, "")
	assert.ErrorContains(t, e, "encoding/hex: invalid byte")
}
