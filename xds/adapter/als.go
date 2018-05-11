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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"

	statsapi "github.com/turbinelabs/api/service/stats/v2"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/nonstdlib/ptr"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/stats"
)

const (
	unknownBuildVersion = "unknown"
)

func newALS(stats stats.Stats, c alsReporterConfig) (*als, error) {
	source := tbntime.NewSource()
	reporter, err := c.Make(source)
	if err != nil {
		return nil, err
	}

	return &als{
		stats:    stats,
		time:     source,
		reporter: reporter,
	}, nil
}

type als struct {
	stats    stats.Stats
	time     tbntime.Source
	reporter *alsReporter
}

type streamMetadata struct {
	zoneName     string
	proxyName    string
	nodeName     string
	logID        string
	buildVersion string
}

func newStreamMetadata(msg *accesslog.StreamAccessLogsMessage) (*streamMetadata, error) {
	identifier := msg.GetIdentifier()
	if identifier == nil {
		return nil, errors.New("missing log identifier")
	}

	node := identifier.GetNode()
	if node == nil {
		return nil, errors.New("missing log identifier.node")
	}

	proxyRef := proxyRefFromNode(node)

	m := &streamMetadata{
		proxyName:    proxyRef.Name(),
		nodeName:     node.Id,
		logID:        identifier.LogName,
		buildVersion: node.BuildVersion,
	}

	if proxyRef.ZoneRef() != nil {
		m.zoneName = proxyRef.ZoneRef().Name()
	}

	if m.zoneName == "" {
		return nil, errors.New("missing log identifier.node.locality.zone")
	}

	if m.proxyName == "" {
		return nil, errors.New("missing log identifier.node.cluster")
	}

	if m.nodeName == "" {
		return nil, errors.New("missing log identifier.node.id")
	}

	if m.logID == "" {
		return nil, errors.New("missing log identifier.logName")
	}

	if m.buildVersion == "" {
		m.buildVersion = unknownBuildVersion
	}

	return m, nil
}

func (m *streamMetadata) Equal(other *streamMetadata) bool {
	if m == nil {
		return false
	} else if other == nil {
		return false
	}

	return *m == *other
}

func (a *als) StreamAccessLogs(stream accesslog.AccessLogService_StreamAccessLogsServer) error {
	var metadata *streamMetadata

	for {
		msg, err := stream.Recv()
		if err != nil {
			console.Error().Printf("stream access logs recv error: %v", err)
			return err
		}

		if metadata == nil {
			// First message needs to tell us about the log stream.
			var err error
			if metadata, err = newStreamMetadata(msg); err != nil {
				console.Error().Println(err.Error())
				return err
			}
		} else if msg.Identifier != nil {
			newMetadata, err := newStreamMetadata(msg)
			if err != nil {
				console.Error().Println(err.Error())
				return err
			}

			if !metadata.Equal(newMetadata) {
				console.Error().Printf(
					"stream metadata changed from %+v to %+v",
					metadata,
					newMetadata,
				)
				metadata = newMetadata
			}
		}

		switch entries := msg.LogEntries.(type) {
		case *accesslog.StreamAccessLogsMessage_HttpLogs:
			if entries.HttpLogs != nil {
				for _, entry := range entries.HttpLogs.LogEntry {
					if entry != nil {
						if err := a.record(metadata, entry); err != nil {
							return err
						}
					}
				}
			}
		default:
			return fmt.Errorf("unexpected log type: %T", msg.LogEntries)
		}
	}
}

func (a *als) record(metadata *streamMetadata, entry *filter.HTTPAccessLogEntry) error {
	isUpstream := metadata.logID == grpcUpstreamLogID
	if !isUpstream && metadata.logID != grpcAccessLogID {
		return fmt.Errorf("invalid log ID: %q", metadata.logID)
	}

	common := entry.CommonProperties
	req := entry.Request
	resp := entry.Response
	if common == nil {
		console.Error().Printf("log entry missing common properties")
		return nil
	}
	if req == nil {
		console.Error().Printf("log entry missing request properties")
		return nil
	}
	if resp == nil {
		console.Error().Printf("log entry missing response properties")
		return nil
	}

	var startTime time.Time
	if common.StartTime != nil {
		startTime = *common.StartTime
	} else {
		startTime = a.time.Now()
	}

	timestamp := strconv.FormatInt(tbntime.ToUnixMilli(startTime), 10)
	proxyVersion := fmt.Sprintf(
		"%s/rotor=%s",
		metadata.buildVersion,
		constants.TbnPublicVersion,
	)

	tags := []stats.Tag{
		stats.NewKVTag(stats.TimestampTag, timestamp),
		stats.NewKVTag(stats.ZoneTag, metadata.zoneName),
		stats.NewKVTag(stats.ProxyTag, metadata.proxyName),
		stats.NewKVTag(stats.ProxyVersionTag, proxyVersion),
		stats.NewKVTag(stats.NodeTag, metadata.nodeName),
		stats.NewKVTag(statsapi.Method, req.RequestMethod.String()),
		stats.NewKVTag(statsapi.Domain, header(req.RequestHeaders, headerDomainKey)),
		stats.NewKVTag(statsapi.RouteKey, header(req.RequestHeaders, headerRouteKey)),
		stats.NewKVTag(statsapi.Rule, header(req.RequestHeaders, headerRuleKey)),
		stats.NewKVTag(statsapi.SharedRule, header(req.RequestHeaders, headerSharedRulesKey)),
		stats.NewKVTag(statsapi.Constraint, header(req.RequestHeaders, headerConstraintKey)),
		stats.NewKVTag(statsapi.Upstream, common.UpstreamCluster),
		stats.NewKVTag(statsapi.Instance, addrToString(common.UpstreamRemoteAddress)),
	}

	respCode := uint32(0)
	if resp.ResponseCode != nil {
		respCode = resp.ResponseCode.Value
	}
	responseTags := append(tags, stats.NewKVTag(statsapi.StatusCode, fmt.Sprintf("%d", respCode)))

	if isUpstream {
		duration := ptr.DurationValue(common.TimeToLastUpstreamRxByte)

		a.stats.Count(statsapi.UpstreamRequests, 1.0, tags...)
		a.stats.Count(statsapi.UpstreamResponses, 1.0, responseTags...)
		a.stats.Timing(statsapi.UpstreamLatency, duration, tags...)

		a.reporter.reportUpstreamLog(req, resp)
	} else {
		duration := ptr.DurationValue(common.TimeToLastDownstreamTxByte)

		a.stats.Count(statsapi.Requests, 1.0, tags...)
		a.stats.Count(statsapi.Responses, 1.0, responseTags...)
		a.stats.Timing(statsapi.Latency, duration, tags...)

		a.reporter.reportAccessLog(req, resp)
	}

	return nil
}

func addrToString(addr *core.Address) string {
	if addr == nil {
		return "-"
	}

	socketAddr := addr.GetSocketAddress()
	if socketAddr == nil {
		// Probably a Pipe.
		return "-"
	}

	port := socketAddr.GetPortValue()
	if port > 0 {
		return fmt.Sprintf("%s:%d", socketAddr.Address, port)
	}

	namedPort := socketAddr.GetNamedPort()
	if namedPort == "" {
		console.Error().Printf("socket address %q has no port value or named port", socketAddr)
		return fmt.Sprintf("%s:-", socketAddr.Address)
	}

	return fmt.Sprintf("%s:%s", socketAddr.Address, namedPort)
}

func header(headers map[string]string, name string) string {
	if value, ok := headers[name]; ok {
		return value
	}

	return "-"
}
