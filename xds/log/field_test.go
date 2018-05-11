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

const testFieldID fieldID = 100

func TestCounter(t *testing.T) {
	assert.Equal(t, counter(testFieldID), field{id: testFieldID, fieldType: counterType})
}

func TestGauge(t *testing.T) {
	assert.Equal(t, gauge(testFieldID), field{id: testFieldID, fieldType: gaugeType})
}

func TestTag(t *testing.T) {
	assert.Equal(t, tag(testFieldID), field{id: testFieldID, fieldType: tagType})
}

func TestTimestamp(t *testing.T) {
	assert.Equal(t, timestamp(testFieldID), field{id: testFieldID, fieldType: timestampType})
}

func TestIgnored(t *testing.T) {
	assert.Equal(t, ignored(testFieldID), field{id: testFieldID, fieldType: ignoredType})
}

func TestQuoted(t *testing.T) {
	testField := ignored(testFieldID)
	quoted := quoted(testField)
	assert.Equal(t, quoted, field{id: testFieldID, fieldType: ignoredType, quoted: true})
	assert.NotEqual(t, testField, quoted)
}
