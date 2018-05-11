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

func TestDefaultAccessLogFormat(t *testing.T) {
	fmt, err := PositionalFormat(TbnAccessFormat)
	assert.Nil(t, err)
	assert.Equal(
		t,
		fmt,
		`&timestamp ^request_id ^remote_addr %method ^request_uri #status ^response_length *response_time "%upstream" "%instance" "%domain" "%route" "%rule" "%shared_rule" "%constraint" "^http_referer" "^http_user_agent"`,
	)
}

func TestDefaultUpstreamLogFormat(t *testing.T) {
	fmt, err := PositionalFormat(TbnUpstreamFormat)
	assert.Nil(t, err)
	assert.Equal(
		t,
		fmt,
		`&timestamp ^request_id #upstream.status "%instance" %method ^upstream.response_length *upstream.response_time "%upstream" "%domain" "%route" "%rule" "%shared_rule" "%constraint"`,
	)
}
