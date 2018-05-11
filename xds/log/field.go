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

import "fmt"

// FieldID is a unique identifier for an Envoy log field.
type fieldID int

// Predefined fieldIDs
const (
	timestampField fieldID = iota
	requestIDField
	requestURIField
	requestLengthField
	requestReferrerField
	requestUserAgentField
	requestRemoteAddrField
	responseStatusField
	responseLengthField
	responseTimeField
	upstreamResponseStatusField
	upstreamResponseLengthField
	upstreamResponseTimeField
	upstreamInstanceField
	upstreamClusterField
	domainField
	routeField
	ruleField
	sharedRuleField
	methodField
	constraintField
)

// FieldType represents a field's data type.
type fieldType int

const (
	// CounterType specifies a counter field.
	counterType fieldType = iota

	// DurationType specifies a duration measurement.
	durationType

	// GaugeType specifies a point-in-time measurement.
	gaugeType

	// TagType specifies a tag.
	tagType

	// TimestampType specifies a timestamp.
	timestampType

	// IgnoredType specifies an ignored field.
	ignoredType
)

func (t fieldType) formatSpecifier() string {
	switch t {
	case counterType:
		return "#"
	case durationType:
		return "*"
	case gaugeType:
		return "$"
	case tagType:
		return "%"
	case timestampType:
		return "&"
	case ignoredType:
		return "^"
	default:
		panic(fmt.Sprintf("unknown field type: %v", t))
	}
}

// Field represents a fieldID with a specific fieldType. The field may
// also be designated as quoted.
type field struct {
	id        fieldID
	fieldType fieldType
	quoted    bool
}

// Counter generates a counter field for a PositionalParser log format
// specifier.
func counter(id fieldID) field {
	return field{id: id, fieldType: counterType}
}

// Duration generates a duration field for a PositionalParser log
// format specifier.
func duration(id fieldID) field {
	return field{id: id, fieldType: durationType}
}

// Gauge generates a gauge field for a PositionalParser log format
// specifier.
func gauge(id fieldID) field {
	return field{id: id, fieldType: gaugeType}
}

// Tag generates a tag field for a PositionalParser log format
// specifier.
func tag(id fieldID) field {
	return field{id: id, fieldType: tagType}
}

// Timestamp generates a timestamp field for a PositionalParser log
// format specifier.
func timestamp(id fieldID) field {
	return field{id: id, fieldType: timestampType}
}

// Ignored generates an ignored field for a PositionalParser log
// format specifier.
func ignored(id fieldID) field {
	return field{id: id, fieldType: ignoredType}
}

// Quoted generates a new field with quoting enabled.
func quoted(field field) field {
	field.quoted = true
	return field
}
