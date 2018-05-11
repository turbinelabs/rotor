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

package log

import (
	"fmt"
	"strings"
)

// Format is a log format definition.
type Format []field

// FieldNameMap is a mapping of FieldID to field name.
type fieldNameMap map[fieldID]string

const (
	// TurbineStatsServerMapping is the name of a mapping of
	// predefined FieldIDs to metric/tag names expected by the Turbine
	// Labs stats forwarder API.
	turbineStatsServerMapping = "tbn_stats"

	// EnvoyLogMapping is the name of a mapping of predefined FieldIDs
	// to Envoy log format field specifiers.
	envoyLogMapping = "envoy_log"

	// EnvoyGRPCRequestHeaderMapping is the name of a mapping of predefined
	// FieldIDs to Envoy GRPC log request header specifiers.
	envoyGRPCRequestHeaderMapping = "envoy_grpc_request_header"

	// TemplateMapping is the name of a mapping of predefined FieldIDs
	// to text/template actions.
	templateMapping = "template"
)

var (
	mappingRegistry map[string]fieldNameMap
)

func init() {
	resetFieldMappings()
}

// ResetFieldMappings resets all field name mappings to their
// defaults. Any custom mappings are lost.
func resetFieldMappings() {
	mappingRegistry = map[string]fieldNameMap{
		// Maps field IDs to the strings expected by the stats API.
		turbineStatsServerMapping: {
			timestampField:              "timestamp",
			requestIDField:              "request_id",
			requestURIField:             "request_uri",
			requestLengthField:          "request_length",
			requestReferrerField:        "http_referer",
			requestUserAgentField:       "http_user_agent",
			requestRemoteAddrField:      "remote_addr",
			responseStatusField:         "status",
			responseLengthField:         "response_length",
			responseTimeField:           "response_time",
			upstreamResponseStatusField: "upstream.status",
			upstreamResponseLengthField: "upstream.response_length",
			upstreamResponseTimeField:   "upstream.response_time",
			upstreamInstanceField:       "instance",
			upstreamClusterField:        "upstream",
			domainField:                 "domain",
			routeField:                  "route",
			ruleField:                   "rule",
			sharedRuleField:             "shared_rule",
			methodField:                 "method",
			constraintField:             "constraint",
		},

		// Maps field IDs to Envoy log format specifiers.
		envoyLogMapping: {
			timestampField:              "%START_TIME%",
			requestIDField:              "%REQ(X-REQUEST-ID)%",
			requestURIField:             "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
			requestLengthField:          "%BYTES_RECEIVED%",
			requestReferrerField:        "%REQ(HTTP-REFERER)%",
			requestUserAgentField:       "%REQ(USER-AGENT)%",
			requestRemoteAddrField:      "%DOWNSTREAM_REMOTE_ADDRESS%",
			responseStatusField:         "%RESPONSE_CODE%",
			responseLengthField:         "%BYTES_SENT%",
			responseTimeField:           "%DURATION%",
			upstreamResponseStatusField: "%RESPONSE_CODE%",
			upstreamResponseLengthField: "%BYTES_RECEIVED%",
			upstreamResponseTimeField:   "%DURATION%",
			upstreamInstanceField:       "%UPSTREAM_HOST%",
			upstreamClusterField:        "%UPSTREAM_CLUSTER%",
			domainField:                 "%REQ(X-TBN-DOMAIN)%",
			routeField:                  "%REQ(X-TBN-ROUTE)%",
			ruleField:                   "%REQ(X-TBN-RULE)%",
			sharedRuleField:             "%REQ(X-TBN-SHARED-RULES)%",
			methodField:                 "%REQ(:METHOD)%",
			constraintField:             "%REQ(X-TBN-CONSTRAINT)%",
		},

		// Maps field IDs to Envoy GRPC log request header names.
		envoyGRPCRequestHeaderMapping: {
			domainField:     "X-TBN-DOMAIN",
			routeField:      "X-TBN-ROUTE",
			ruleField:       "X-TBN-RULE",
			sharedRuleField: "X-TBN-SHARED-RULES",
			constraintField: "X-TBN-CONSTRAINT",
		},

		templateMapping: {
			timestampField:              "{{.Timestamp}}",
			requestIDField:              "{{.RequestID}}",
			requestURIField:             "{{.Path}}",
			requestLengthField:          "{{.RequestLength}}",
			requestReferrerField:        "{{.HTTPReferer}}",
			requestUserAgentField:       "{{.HTTPUserAgent}}",
			requestRemoteAddrField:      "{{.RemoteIP}}",
			responseStatusField:         "{{.StatusCode}}",
			responseLengthField:         "{{.ResponseLength}}",
			responseTimeField:           "{{.ResponseTime}}",
			upstreamResponseStatusField: "{{.StatusCode}}",
			upstreamResponseLengthField: "{{.ResponseLength}}",
			upstreamResponseTimeField:   "{{.ResponseTime}}",
			upstreamInstanceField:       "{{.InstanceAddr}}",
			upstreamClusterField:        "{{.Upstream}}",
			domainField:                 "{{.Domain}}",
			routeField:                  "{{.Route}}",
			ruleField:                   "{{.Rule}}",
			sharedRuleField:             "{{.SharedRule}}",
			methodField:                 "{{.Method}}",
			constraintField:             "{{.Constraint}}",
		},
	}
}

// PositionalFormat composes a sequence of Fields into a log format
// specifier for a logparser/parser.PositionalParser. Fields in the
// format are delimited by a single space.
func PositionalFormat(format Format) (string, error) {
	if len(format) == 0 {
		return "", nil
	}

	mapping := mappingRegistry[turbineStatsServerMapping]

	fieldStrings := make([]string, len(format))
	for i, field := range format {
		name, ok := mapping[field.id]
		if !ok {
			return "", fmt.Errorf("unknown field id %d: %v", field.id, field)
		}

		f := fmt.Sprintf(`%s%s`, field.fieldType.formatSpecifier(), name)

		if field.quoted {
			f = fmt.Sprintf(`"%s"`, f)
		}

		fieldStrings[i] = f
	}

	return strings.Join(fieldStrings, " "), nil
}

func trivialFormat(mapping fieldNameMap, format Format) (string, error) {
	if len(format) == 0 {
		return "", nil
	}

	fieldStrings := make([]string, len(format))
	for i, field := range format {
		name, ok := mapping[field.id]
		if !ok {
			return "", fmt.Errorf("unknown field id %d: %v", field.id, field)
		}

		if field.quoted {
			name = fmt.Sprintf(`"%s"`, name)
		}

		fieldStrings[i] = name
	}

	return strings.Join(fieldStrings, " ") + "\n", nil
}

// EnvoyLogFormat composes a sequence of Fields into an Envoy log
// format specifier. Fields in the format are delimited by a single
// space and a non-empty format is always terminated with a new line.
func EnvoyLogFormat(format Format) (string, error) {
	return trivialFormat(mappingRegistry[envoyLogMapping], format)
}

// EnvoyGRPCRequestHeaders returns an array of strings containing request header
// names to be added to the Envoy GRPC log configuration.
func EnvoyGRPCRequestHeaders(format Format) []string {
	if len(format) == 0 {
		return nil
	}

	mapping := mappingRegistry[envoyGRPCRequestHeaderMapping]

	headers := make([]string, 0, len(mapping))
	for _, field := range format {
		name, ok := mapping[field.id]
		if !ok {
			continue
		}

		headers = append(headers, name)
	}

	return headers
}

// TemplateFormat composes a log line template for the given
// Format. Fields in the format are delimited by a single space and a
// non-empty format is always terminated with a new line.
func TemplateFormat(format Format) (string, error) {
	return trivialFormat(mappingRegistry[templateMapping], format)
}
