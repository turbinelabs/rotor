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
	"testing"

	"github.com/turbinelabs/test/assert"
)

const (
	testFieldTimestamp fieldID = iota + 100
	testFieldCounter
	testFieldGauge
	testFieldTag
	testFieldIgnored
	testFieldUnknown
)

func TestPositionalFormat(t *testing.T) {
	fmt, err := PositionalFormat(Format{})
	assert.Nil(t, err)
	assert.Equal(t, fmt, "")

	fmt, err = PositionalFormat(Format{counter(testFieldUnknown)})
	assert.NonNil(t, err)
	assert.Equal(t, fmt, "")

	defer resetFieldMappings()
	mappingRegistry[turbineStatsServerMapping][testFieldTimestamp] = "ts"
	mappingRegistry[turbineStatsServerMapping][testFieldCounter] = "c"
	mappingRegistry[turbineStatsServerMapping][testFieldGauge] = "g"
	mappingRegistry[turbineStatsServerMapping][testFieldTag] = "tg"
	mappingRegistry[turbineStatsServerMapping][testFieldIgnored] = "i"

	fmt, err = PositionalFormat(Format{
		timestamp(testFieldTimestamp),
		counter(testFieldCounter),
		gauge(testFieldGauge),
		quoted(tag(testFieldTag)),
		ignored(testFieldIgnored),
	})

	assert.Nil(t, err)
	assert.Equal(t, fmt, `&ts #c $g "%tg" ^i`)
}

func TestEnvoyLogFormat(t *testing.T) {
	fmt, err := EnvoyLogFormat(Format{
		timestamp(timestampField),
		quoted(ignored(requestIDField)),
		counter(responseStatusField),
		tag(methodField),
	})
	assert.Nil(t, err)
	assert.Equal(t, fmt, `%START_TIME% "%REQ(X-REQUEST-ID)%" %RESPONSE_CODE% %REQ(:METHOD)%`+"\n")

	fmt, err = EnvoyLogFormat(Format{})
	assert.Nil(t, err)
	assert.Equal(t, fmt, "")

	fmt, err = EnvoyLogFormat(Format{counter(testFieldUnknown)})
	assert.NonNil(t, err)
	assert.Equal(t, fmt, "")
}

func TestEnvoyGRPCRequestHeaders(t *testing.T) {
	headers := EnvoyGRPCRequestHeaders(Format{
		timestamp(timestampField),
		quoted(tag(domainField)),
		tag(routeField),
		tag(testFieldUnknown),
	})
	assert.ArrayEqual(t, headers, []string{"X-TBN-DOMAIN", "X-TBN-ROUTE"})

	assert.Nil(t, EnvoyGRPCRequestHeaders(Format{}))
}

func TestTemplateFormat(t *testing.T) {
	fmt, err := TemplateFormat(Format{
		timestamp(timestampField),
		quoted(ignored(requestIDField)),
		counter(responseStatusField),
		tag(methodField),
	})
	assert.Nil(t, err)
	assert.Equal(t, fmt, `{{.Timestamp}} "{{.RequestID}}" {{.StatusCode}} {{.Method}}`+"\n")
}
