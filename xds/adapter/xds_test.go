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
	"syscall"
	"testing"
	"time"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoyfilter "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	envoyals "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"

	"github.com/turbinelabs/nonstdlib/ptr"
	tbnstrings "github.com/turbinelabs/nonstdlib/strings"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

func TestNewXDS(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)

	x, err := NewXDS(
		":0",
		poller.NewNopRegistrar(),
		"",
		defaultDefaultTimeout,
		mockStats,
		WithTopResponseLog(-1, time.Second),
	)
	assert.NonNil(t, x)
	assert.Nil(t, err)
	xdsImpl := x.(*xds)
	assert.NonNil(t, xdsImpl.logServer)

	x, err = NewXDS(
		":0",
		poller.NewNopRegistrar(),
		"",
		defaultDefaultTimeout,
		mockStats,
	)
	assert.NonNil(t, x)
	assert.Nil(t, err)
	xdsImpl = x.(*xds)
	assert.NonNil(t, xdsImpl.logServer)
}

func TestXDSLifecycle(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)
	mockStats.EXPECT().Close().Return(nil)

	xds, err := NewXDS(
		":0",
		poller.NewNopRegistrar(),
		"",
		defaultDefaultTimeout,
		mockStats,
	)
	assert.Nil(t, err)

	runError := make(chan error, 1)
	go func() {
		defer close(runError)
		runError <- xds.Run()
	}()

	resolvedAddr := xds.Addr()
	assert.NotEqual(t, resolvedAddr, "")
	_, port, err := tbnstrings.SplitHostPort(resolvedAddr)
	assert.Nil(t, err)
	assert.NotEqual(t, port, 0)

	// Subsequent calls do not block
	assert.Equal(t, xds.Addr(), resolvedAddr)

	assert.ChannelEmpty(t, runError)

	conn, err := grpc.Dial(resolvedAddr, grpc.WithInsecure(), grpc.WithBlock())
	assert.Nil(t, err)

	client := envoyapi.NewListenerDiscoveryServiceClient(conn)
	client.FetchListeners(context.TODO(), &envoyapi.DiscoveryRequest{})

	assert.Nil(t, conn.Close())

	xds.Stop()

	// Ignore the error Serve (and therefore Run) returns on Stop.
	<-runError
}

func TestXDSLifecycleWithStreamingLogs(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	statsSent := make(chan struct{}, 1)

	anyTags := []interface{}{}
	for i := 0; i < numALSTags; i++ {
		anyTags = append(anyTags, gomock.Any())
	}
	anyRespTags := append(anyTags, gomock.Any())

	mockStats := stats.NewMockStats(ctrl)
	mockStats.EXPECT().Count(gomock.Any(), gomock.Any(), anyTags...).MinTimes(1)      // requests
	mockStats.EXPECT().Count(gomock.Any(), gomock.Any(), anyRespTags...).MinTimes(1)  // responses
	mockStats.EXPECT().Timing(gomock.Any(), gomock.Any(), anyTags...).MinTimes(1).Do( // latency
		func(
			metric string,
			value time.Duration,
			t1, t2, t3, t4, t5, t6, t7, t8, t9, t10, t11, t12, t13 stats.Tag,
		) {
			close(statsSent)
		})
	mockStats.EXPECT().Close().Return(nil)

	xds, err := NewXDS(
		":0",
		poller.NewNopRegistrar(),
		"",
		defaultDefaultTimeout,
		mockStats,
	)
	assert.Nil(t, err)

	runError := make(chan error, 1)
	go func() {
		defer close(runError)
		runError <- xds.Run()
	}()

	resolvedAddr := xds.Addr()
	assert.NotEqual(t, resolvedAddr, "")
	_, port, err := tbnstrings.SplitHostPort(resolvedAddr)
	assert.Nil(t, err)
	assert.NotEqual(t, port, 0)

	// Subsequent calls do not block
	assert.Equal(t, xds.Addr(), resolvedAddr)

	assert.ChannelEmpty(t, runError)

	conn, err := grpc.Dial(resolvedAddr, grpc.WithInsecure(), grpc.WithBlock())
	assert.Nil(t, err)

	client := envoyals.NewAccessLogServiceClient(conn)
	callClient, err := client.StreamAccessLogs(context.TODO())
	assert.Nil(t, err)

	err = callClient.Send(
		&envoyals.StreamAccessLogsMessage{
			Identifier: &envoyals.StreamAccessLogsMessage_Identifier{
				Node: &envoycore.Node{
					Id:      "node-id",
					Cluster: "proxy-name",
					Locality: &envoycore.Locality{
						Zone: "zone-name",
					},
				},
				LogName: grpcUpstreamLogID,
			},
			LogEntries: &envoyals.StreamAccessLogsMessage_HttpLogs{
				HttpLogs: &envoyals.StreamAccessLogsMessage_HTTPAccessLogEntries{
					LogEntry: []*envoyfilter.HTTPAccessLogEntry{
						{
							CommonProperties: &envoyfilter.AccessLogCommon{
								StartTime:       ptr.Time(time.Now()),
								UpstreamCluster: "upstream",
								UpstreamRemoteAddress: &envoycore.Address{
									&envoycore.Address_SocketAddress{
										SocketAddress: &envoycore.SocketAddress{
											Address:       "1.2.3.4",
											PortSpecifier: &envoycore.SocketAddress_PortValue{9999},
										},
									},
								},
								TimeToLastDownstreamTxByte: ptr.Duration(1 * time.Second),
							},
							Request: &envoyfilter.HTTPRequestProperties{
								RequestMethod: envoycore.POST,
								RequestHeaders: map[string]string{
									headerDomainKey:      "domain",
									headerRouteKey:       "route",
									headerRuleKey:        "rule",
									headerSharedRulesKey: "shared-rule",
									headerConstraintKey:  "constraint",
								},
							},
							Response: &envoyfilter.HTTPResponseProperties{
								ResponseCode: &types.UInt32Value{200},
							},
						},
					},
				},
			},
		},
	)
	assert.Nil(t, err)

	<-statsSent

	assert.Nil(t, conn.Close())

	xds.Stop()

	// Ignore the error Serve (and therefore Run) returns on Stop.
	<-runError
}

func TestXDSLifecycleWithSignal(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStats := stats.NewMockStats(ctrl)
	mockStats.EXPECT().Close().Return(nil)

	x, err := NewXDS(
		":0",
		poller.NewNopRegistrar(),
		"",
		defaultDefaultTimeout,
		mockStats,
	)
	assert.Nil(t, err)

	runError := make(chan error, 1)
	go func() {
		defer close(runError)
		runError <- x.Run()
	}()

	resolvedAddr := x.Addr()
	assert.NotEqual(t, resolvedAddr, "")
	_, port, err := tbnstrings.SplitHostPort(resolvedAddr)
	assert.Nil(t, err)
	assert.NotEqual(t, port, 0)

	// Subsequent calls do not block
	assert.Equal(t, x.Addr(), resolvedAddr)

	assert.ChannelEmpty(t, runError)

	conn, err := grpc.Dial(resolvedAddr, grpc.WithInsecure(), grpc.WithBlock())
	assert.Nil(t, err)

	client := envoyapi.NewListenerDiscoveryServiceClient(conn)
	client.FetchListeners(context.TODO(), &envoyapi.DiscoveryRequest{})

	assert.Nil(t, conn.Close())

	// simulated signal
	x.(*xds).signalChan <- syscall.SIGINT

	// Ignore the error Serve (and therefore Run) returns on Stop.
	<-runError
}
