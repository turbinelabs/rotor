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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoylog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	"google.golang.org/grpc/metadata"

	statsapi "github.com/turbinelabs/api/service/stats/v2"
	"github.com/turbinelabs/nonstdlib/ptr"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

const (
	numALSTags = 13
)

type mockSrv struct {
	messages []interface{}
}

func (m *mockSrv) SetHeader(x metadata.MD) error  { panic("unexpected call") }
func (m *mockSrv) SendHeader(x metadata.MD) error { panic("unexpected call") }
func (m *mockSrv) SetTrailer(x metadata.MD)       { panic("unexpected call") }
func (m *mockSrv) Context() context.Context       { panic("unexpected call") }
func (m *mockSrv) SendMsg(x interface{}) error    { panic("unexpected call") }
func (m *mockSrv) RecvMsg(x interface{}) error    { panic("unexpected call") }

func (m *mockSrv) SendAndClose(x *accesslog.StreamAccessLogsResponse) error {
	panic("unexpected call")
}

func (m *mockSrv) Recv() (*accesslog.StreamAccessLogsMessage, error) {
	if len(m.messages) == 0 {
		panic("ran out of messages")
	}

	var result interface{}
	result, m.messages = m.messages[0], m.messages[1:]

	switch v := result.(type) {
	case *accesslog.StreamAccessLogsMessage:
		return v, nil
	case error:
		return nil, v
	}

	panic(fmt.Sprintf("unexpected message type: %T", result))
}

func mkALS(ctrl *gomock.Controller) (*als, *stats.MockStats) {
	mockStats := stats.NewMockStats(ctrl)
	return &als{
		stats:    mockStats,
		time:     tbntime.NewSource(),
		reporter: &alsReporter{},
	}, mockStats
}

func TestStreamAccessLogsRecvError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	als, _ := mkALS(ctrl)

	mockSrv := &mockSrv{[]interface{}{errors.New("boom")}}
	err := als.StreamAccessLogs(mockSrv)
	assert.ErrorContains(t, err, "boom")
}

func TestStreamAccessLogsErrors(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	als, _ := mkALS(ctrl)

	expectedErrors := []string{
		"missing log identifier",
		"missing log identifier.node",
		"missing log identifier.node.id",
		"missing log identifier.logName",
		"unexpected log type: ",
		`invalid log ID: "unknown"`,
	}

	node := &core.Node{
		Id:      "node-id",
		Cluster: "proxy-name",
		Locality: &core.Locality{
			Zone: "zone-name",
		},
		BuildVersion: "build-version",
	}

	mockSrv := &mockSrv{[]interface{}{
		// missing identifier
		&accesslog.StreamAccessLogsMessage{},

		// missing identifier.node
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				LogName: "id",
			},
		},

		// missing identifier.node.id
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node: &core.Node{
					Locality: &core.Locality{Zone: "zone-name"},
					Cluster:  "proxy-name",
				},
				LogName: "id",
			},
		},

		// missing identifier.logName
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node: &core.Node{
					Locality: &core.Locality{Zone: "zone-name"},
					Cluster:  "proxy-name",
					Id:       "node-id",
				},
			},
		},

		// wrong log entries type
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: "id",
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_TcpLogs{},
		},

		// unknown log id
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: "unknown",
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoylog.HTTPAccessLogEntry{{}},
				},
			},
		},
	}}

	for _, expectedError := range expectedErrors {
		err := als.StreamAccessLogs(mockSrv)
		assert.ErrorContains(t, err, expectedError)
	}
}

func TestStreamAccessLogsIgnoresBadData(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	als, _ := mkALS(ctrl)

	node := &core.Node{
		Id:      "node-id",
		Cluster: "proxy-name",
		Locality: &core.Locality{
			Zone: "zone-name",
		},
		BuildVersion: "build-version",
	}

	mockSrv := &mockSrv{[]interface{}{
		// no entries
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: "id",
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{},
		},

		// still no entries
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: "id",
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{},
			},
		},

		// nil entries
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: "id",
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoylog.HTTPAccessLogEntry{nil, nil, nil},
				},
			},
		},

		// missing common properties
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: grpcAccessLogID,
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoylog.HTTPAccessLogEntry{{}},
				},
			},
		},

		// missing request properties
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: grpcAccessLogID,
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoylog.HTTPAccessLogEntry{
						{
							CommonProperties: &envoylog.AccessLogCommon{},
						},
					},
				},
			},
		},

		// missing response properties
		&accesslog.StreamAccessLogsMessage{
			Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
				Node:    node,
				LogName: grpcAccessLogID,
			},
			LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoylog.HTTPAccessLogEntry{
						{
							CommonProperties: &envoylog.AccessLogCommon{},
							Request:          &envoylog.HTTPRequestProperties{},
						},
					},
				},
			},
		},

		errors.New("terminate"),
	}}

	err := als.StreamAccessLogs(mockSrv)
	assert.ErrorContains(t, err, "terminate")
}

func testStreamAccessLogs(t *testing.T, mode string, setStartTime bool) {
	if mode != grpcAccessLogID && mode != grpcUpstreamLogID {
		assert.Tracing(t).Fatalf("bad mode: %q", mode)
	}

	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	node := &core.Node{
		Id:      "node-id",
		Cluster: "proxy-name",
		Locality: &core.Locality{
			Zone: "zone-name",
		},
		BuildVersion: "build-version",
	}

	tbntime.WithCurrentTimeFrozen(func(tc tbntime.ControlledSource) {
		expectedMillis := int64(1522794000000)
		ts := ptr.Time(tbntime.FromUnixMilli(expectedMillis))

		if !setStartTime {
			ts = nil
			expectedMillis = tc.Now().UnixNano() / int64(time.Millisecond)
		}

		als, mockStats := mkALS(ctrl)
		als.time = tc

		mockSrv := &mockSrv{[]interface{}{
			&accesslog.StreamAccessLogsMessage{
				Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
					Node:    node,
					LogName: mode,
				},
				LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
					HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
						LogEntry: []*envoylog.HTTPAccessLogEntry{
							{
								CommonProperties: &envoylog.AccessLogCommon{
									StartTime:       ts,
									UpstreamCluster: "upstream",
									UpstreamRemoteAddress: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       "1.2.3.4",
												PortSpecifier: &core.SocketAddress_PortValue{PortValue: 9999},
											},
										},
									},
									TimeToLastDownstreamTxByte: ptr.Duration(2 * time.Second),
									TimeToLastUpstreamRxByte:   ptr.Duration(1 * time.Second),
								},
								Request: &envoylog.HTTPRequestProperties{
									RequestMethod: core.POST,
									// Envoy delivers these in lower case.
									RequestHeaders: map[string]string{
										strings.ToLower(headerDomainKey):      "domain",
										strings.ToLower(headerRouteKey):       "route",
										strings.ToLower(headerRuleKey):        "rule",
										strings.ToLower(headerSharedRulesKey): "shared-rule",
										strings.ToLower(headerConstraintKey):  "constraint",
									},
								},
								Response: &envoylog.HTTPResponseProperties{
									ResponseCode: &types.UInt32Value{Value: 200, XXX_NoUnkeyedLiteral: struct{}{}, XXX_sizecache: 32},
								},
							},
						},
					},
				},
			},

			errors.New("terminate"),
		}}

		expectedTags := []interface{}{
			stats.NewKVTag(stats.TimestampTag, fmt.Sprintf("%d", expectedMillis)),
			stats.NewKVTag(stats.ZoneTag, "zone-name"),
			stats.NewKVTag(stats.ProxyTag, "proxy-name"),
			stats.NewKVTag(
				stats.ProxyVersionTag,
				"build-version/rotor="+constants.TbnPublicVersion,
			),
			stats.NewKVTag(stats.NodeTag, "node-id"),
			stats.NewKVTag(statsapi.Method, "POST"),
			stats.NewKVTag(statsapi.Domain, "domain"),
			stats.NewKVTag(statsapi.RouteKey, "route"),
			stats.NewKVTag(statsapi.Rule, "rule"),
			stats.NewKVTag(statsapi.SharedRule, "shared-rule"),
			stats.NewKVTag(statsapi.Constraint, "constraint"),
			stats.NewKVTag(statsapi.Upstream, "upstream"),
			stats.NewKVTag(statsapi.Instance, "1.2.3.4:9999"),
		}

		expectedResponseTags := append(expectedTags, stats.NewKVTag(statsapi.StatusCode, "200"))

		if mode == grpcAccessLogID {
			mockStats.EXPECT().Count(statsapi.Requests, 1.0, expectedTags...)
			mockStats.EXPECT().Count(statsapi.Responses, 1.0, expectedResponseTags...)
			mockStats.EXPECT().Timing(statsapi.Latency, 2*time.Second, expectedTags...)
		} else {
			mockStats.EXPECT().Count(statsapi.UpstreamRequests, 1.0, expectedTags...)
			mockStats.EXPECT().Count(statsapi.UpstreamResponses, 1.0, expectedResponseTags...)
			mockStats.EXPECT().Timing(statsapi.UpstreamLatency, 1*time.Second, expectedTags...)
		}

		err := als.StreamAccessLogs(mockSrv)
		assert.ErrorContains(t, err, "terminate")
	})
}

func TestStreamAccessLogsForAccessLogs(t *testing.T) {
	testStreamAccessLogs(t, grpcAccessLogID, false)
	testStreamAccessLogs(t, grpcAccessLogID, true)
}

func TestStreamAccessLogsForUpstreamLogs(t *testing.T) {
	testStreamAccessLogs(t, grpcUpstreamLogID, false)
	testStreamAccessLogs(t, grpcUpstreamLogID, true)
}

func TestStreamAccessLogsMultipleMessages(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	node := &core.Node{
		Id:      "node-id",
		Cluster: "proxy-name",
		Locality: &core.Locality{
			Zone: "zone-name",
		},
	}

	tbntime.WithCurrentTimeFrozen(func(tc tbntime.ControlledSource) {
		expectedMillis := int64(1522794000000)
		ts := ptr.Time(tbntime.FromUnixMilli(expectedMillis))

		als, mockStats := mkALS(ctrl)
		als.time = tc

		mockSrv := &mockSrv{[]interface{}{
			&accesslog.StreamAccessLogsMessage{
				Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
					Node:    node,
					LogName: grpcAccessLogID,
				},
				LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
					HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
						LogEntry: []*envoylog.HTTPAccessLogEntry{
							{
								CommonProperties: &envoylog.AccessLogCommon{
									StartTime:       ts,
									UpstreamCluster: "upstream",
									UpstreamRemoteAddress: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       "1.2.3.4",
												PortSpecifier: &core.SocketAddress_PortValue{9999},
											},
										},
									},
									TimeToLastDownstreamTxByte: ptr.Duration(2 * time.Second),
									TimeToLastUpstreamRxByte:   ptr.Duration(1 * time.Second),
								},
								Request: &envoylog.HTTPRequestProperties{RequestMethod: core.POST},
								Response: &envoylog.HTTPResponseProperties{
									ResponseCode: &types.UInt32Value{Value: 200, XXX_NoUnkeyedLiteral: struct{}{}, XXX_sizecache: 32},
								},
							},
						},
					},
				},
			},
			&accesslog.StreamAccessLogsMessage{
				Identifier: nil,
				LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
					HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
						LogEntry: []*envoylog.HTTPAccessLogEntry{
							{
								CommonProperties: &envoylog.AccessLogCommon{
									StartTime:       ts,
									UpstreamCluster: "upstream",
									UpstreamRemoteAddress: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       "1.2.3.4",
												PortSpecifier: &core.SocketAddress_PortValue{9999},
											},
										},
									},
									TimeToLastDownstreamTxByte: ptr.Duration(2 * time.Second),
									TimeToLastUpstreamRxByte:   ptr.Duration(1 * time.Second),
								},
								Request: &envoylog.HTTPRequestProperties{RequestMethod: core.POST},
								Response: &envoylog.HTTPResponseProperties{
									ResponseCode: &types.UInt32Value{Value: 200, XXX_NoUnkeyedLiteral: struct{}{}, XXX_sizecache: 32},
								},
							},
						},
					},
				},
			},

			errors.New("terminate"),
		}}

		anyTags := []interface{}{}
		for i := 0; i < numALSTags; i++ {
			anyTags = append(anyTags, gomock.Any())
		}
		anyRespTags := append(anyTags, gomock.Any())

		mockStats.EXPECT().Count(gomock.Any(), gomock.Any(), anyTags...).Times(2)
		mockStats.EXPECT().Count(gomock.Any(), gomock.Any(), anyRespTags...).Times(2)
		mockStats.EXPECT().Timing(gomock.Any(), gomock.Any(), anyTags...).Times(2)

		err := als.StreamAccessLogs(mockSrv)
		assert.ErrorContains(t, err, "terminate")
	})
}

func TestStreamAccessLogsMultipleMessagesAndChangingLogName(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	node := &core.Node{
		Id:      "node-id",
		Cluster: "proxy-name",
		Locality: &core.Locality{
			Zone: "zone-name",
		},
	}

	tbntime.WithCurrentTimeFrozen(func(tc tbntime.ControlledSource) {
		expectedMillis := int64(1522794000000)
		ts := ptr.Time(tbntime.FromUnixMilli(expectedMillis))

		als, mockStats := mkALS(ctrl)
		als.time = tc

		mockSrv := &mockSrv{[]interface{}{
			&accesslog.StreamAccessLogsMessage{
				Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
					Node:    node,
					LogName: grpcAccessLogID,
				},
				LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
					HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
						LogEntry: []*envoylog.HTTPAccessLogEntry{
							{
								CommonProperties: &envoylog.AccessLogCommon{
									StartTime:       ts,
									UpstreamCluster: "upstream",
									UpstreamRemoteAddress: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       "1.2.3.4",
												PortSpecifier: &core.SocketAddress_PortValue{9999},
											},
										},
									},
									TimeToLastDownstreamTxByte: ptr.Duration(2 * time.Second),
									TimeToLastUpstreamRxByte:   ptr.Duration(1 * time.Second),
								},
								Request: &envoylog.HTTPRequestProperties{RequestMethod: core.POST},
								Response: &envoylog.HTTPResponseProperties{
									ResponseCode: &types.UInt32Value{Value: 200, XXX_NoUnkeyedLiteral: struct{}{}, XXX_sizecache: 32},
								},
							},
						},
					},
				},
			},
			&accesslog.StreamAccessLogsMessage{
				Identifier: &accesslog.StreamAccessLogsMessage_Identifier{
					Node:    node,
					LogName: grpcUpstreamLogID,
				},
				LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
					HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
						LogEntry: []*envoylog.HTTPAccessLogEntry{
							{
								CommonProperties: &envoylog.AccessLogCommon{
									StartTime:       ts,
									UpstreamCluster: "upstream",
									UpstreamRemoteAddress: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Address:       "1.2.3.4",
												PortSpecifier: &core.SocketAddress_PortValue{9999},
											},
										},
									},
									TimeToLastDownstreamTxByte: ptr.Duration(2 * time.Second),
									TimeToLastUpstreamRxByte:   ptr.Duration(1 * time.Second),
								},
								Request: &envoylog.HTTPRequestProperties{RequestMethod: core.POST},
								Response: &envoylog.HTTPResponseProperties{
									ResponseCode: &types.UInt32Value{Value: 200, XXX_NoUnkeyedLiteral: struct{}{}, XXX_sizecache: 32},
								},
							},
						},
					},
				},
			},

			errors.New("terminate"),
		}}

		anyTags := []interface{}{}
		for i := 0; i < numALSTags; i++ {
			anyTags = append(anyTags, gomock.Any())
		}

		anyRespTags := append(anyTags, gomock.Any())

		// First message...
		mockStats.EXPECT().Count(statsapi.Requests, gomock.Any(), anyTags...)
		mockStats.EXPECT().Count(statsapi.Responses, gomock.Any(), anyRespTags...)
		mockStats.EXPECT().Timing(statsapi.Latency, gomock.Any(), anyTags...)

		// Second message...
		mockStats.EXPECT().Count(statsapi.UpstreamRequests, gomock.Any(), anyTags...)
		mockStats.EXPECT().Count(statsapi.UpstreamResponses, gomock.Any(), anyRespTags...)
		mockStats.EXPECT().Timing(statsapi.UpstreamLatency, gomock.Any(), anyTags...)

		err := als.StreamAccessLogs(mockSrv)
		assert.ErrorContains(t, err, "terminate")
	})
}

func TestAddrToString(t *testing.T) {
	assert.Equal(t, addrToString(nil), "-")
	assert.Equal(t, addrToString(&core.Address{}), "-")
	assert.Equal(
		t,
		addrToString(&core.Address{
			Address: &core.Address_Pipe{
				Pipe: &core.Pipe{
					Path: "blah",
				},
			},
		}),
		"-",
	)
	assert.Equal(
		t,
		addrToString(&core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address: "2.3.4.5",
				},
			},
		}),
		"2.3.4.5:-",
	)
	assert.Equal(
		t,
		addrToString(&core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:       "2.3.4.5",
					PortSpecifier: &core.SocketAddress_PortValue{PortValue: 8888},
				},
			},
		}),
		"2.3.4.5:8888",
	)
	assert.Equal(
		t,
		addrToString(&core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address:       "2.3.4.5",
					PortSpecifier: &core.SocketAddress_NamedPort{NamedPort: "http"},
				},
			},
		}),
		"2.3.4.5:http",
	)
}

func TestHeader(t *testing.T) {
	headers := map[string]string{"x-header": "value"}

	assert.Equal(t, header(headers, "x-header"), "value")
	assert.Equal(t, header(headers, "x-other"), "-")
}
