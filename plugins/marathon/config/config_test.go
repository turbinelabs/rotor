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

package config

import (
	"testing"
	"time"

	"github.com/turbinelabs/test/assert"
)

func TestNewFromToml(t *testing.T) {
	for _, tc := range []struct {
		name string
		toml string
		conf DCOSConfig
		err  string
	}{
		{
			name: "success (int/bool)",
			toml: `
[some_other_section]
foo = "bar"
bar = "baz"

[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = 5
dcos_acs_token = "yoyodyne"
`,
			conf: DCOSConfig{
				URL:            "http://dcos.snakeoil.mesosphere.com",
				Insecure:       true,
				RequestTimeout: 5 * time.Second,
				ACSToken:       "yoyodyne",
			},
		},
		{
			name: "success (strings)",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = "false"
timeout = "5"
dcos_acs_token = "yoyodyne"
`,
			conf: DCOSConfig{
				URL:            "http://dcos.snakeoil.mesosphere.com",
				Insecure:       true,
				RequestTimeout: 5 * time.Second,
				ACSToken:       "yoyodyne",
			},
		},
		{
			name: "success (defaults)",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
dcos_acs_token = "yoyodyne"
`,
			conf: DCOSConfig{
				URL:            "http://dcos.snakeoil.mesosphere.com",
				Insecure:       false,
				RequestTimeout: 5 * time.Second,
				ACSToken:       "yoyodyne",
			},
		},
		{
			name: "no core",
			toml: `
[some_other_section]
foo = "bar"
bar = "baz"
`,
			err: "could not fild [core] section in dcos.toml",
		},
		{
			name: "missing",
			toml: "[core]",
			err:  "the following fields were missing from dcos.toml: core.dcos_url, core.dcos_acs_token",
		},
		{
			name: "missing and malformed",
			toml: `
[core]
dcos_url = "::not a url::"
ssl_verify = "blarp"
timeout = 0
`,
			err: `the following fields were missing from dcos.toml: core.dcos_acs_token; the following fields were malformed from dcos.toml: core.dcos_url ("::not a url::"), core.ssl_verify ("blarp"); core.timeout must be >= 1`,
		},
		{
			name: "bad URL",
			toml: `
[core]
dcos_url = "::not a url::"
ssl_verify = false
timeout = 5
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.dcos_url ("::not a url::")`,
		},
		{
			name: "bad type for URL",
			toml: `
[core]
dcos_url = 0
ssl_verify = false
timeout = 5
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.dcos_url (0)`,
		},
		{
			name: "bad value for sslVerify",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = "floober"
timeout = 5
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.ssl_verify ("floober")`,
		},
		{
			name: "bad type for sslVerify",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = 5
timeout = 5
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.ssl_verify (5)`,
		}, {
			name: "bad value for timeout",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = "chicken"
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.timeout ("chicken")`,
		},
		{
			name: "bad type for timeout",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = 0.3
dcos_acs_token = "yoyodyne"
`,
			err: `the following fields were malformed from dcos.toml: core.timeout (0.3)`,
		},
		{
			name: "bad value for string timeout",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = "0"
dcos_acs_token = "yoyodyne"
`,
			err: `core.timeout must be >= 1`,
		},
		{
			name: "bad value for int timeout",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = 0
dcos_acs_token = "yoyodyne"
`,
			err: `core.timeout must be >= 1`,
		},
		{
			name: "bad type for token",
			toml: `
[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = "1"
dcos_acs_token = 3
`,
			err: `the following fields were malformed from dcos.toml: core.dcos_acs_token (3)`,
		},
	} {
		assert.Group(
			tc.name,
			t,
			func(g *assert.G) {
				conf, err := NewFromTOML(tc.toml)
				if tc.err == "" {
					assert.Nil(g, err)
				} else {
					assert.ErrorContains(t, err, tc.err)
				}
				assert.Equal(g, conf, tc.conf)
			},
		)
	}
}
