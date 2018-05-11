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

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/tempfile"
)

func TestFromFlags(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()
	ff := NewFromFlags(flagset)
	ffImpl := ff.(*fromFlags)
	assert.Equal(t, ffImpl.url, "")
	assert.Equal(t, ffImpl.token, "")
	assert.Equal(t, ffImpl.tomlFile, "")
	assert.Equal(t, ffImpl.timeout, DefaultRequestTimeout)
	assert.Equal(t, ffImpl.insecure, false)

	flagset.Parse([]string{
		"--dcos.request-timeout=2s",
		"--dcos.acs-token=foo",
		"--dcos.url=bar",
		"--dcos.insecure=true",
		"--dcos.toml-file=baz",
	})

	assert.Equal(t, ffImpl.url, "bar")
	assert.Equal(t, ffImpl.token, "foo")
	assert.Equal(t, ffImpl.tomlFile, "baz")
	assert.Equal(t, ffImpl.timeout, 2*time.Second)
	assert.Equal(t, ffImpl.insecure, true)
}

func TestValidateOK(t *testing.T) {
	ff := &fromFlags{url: "foo", token: "bar", timeout: DefaultRequestTimeout, insecure: true}
	assert.Nil(t, ff.Validate())
}

func TestValidateOKToml(t *testing.T) {
	ff := &fromFlags{tomlFile: "baz"}
	assert.Nil(t, ff.Validate())
}

func TestValidateNothingSet(t *testing.T) {
	ff := &fromFlags{}
	assert.ErrorContains(t, ff.Validate(), "Must specify either --dcos.toml-file or both --dcos.url and --dcos.acs-token")
}

func TestValidateCollision(t *testing.T) {
	ff := &fromFlags{url: "foo", token: "bar", tomlFile: "baz"}
	assert.ErrorContains(t, ff.Validate(), "Must specify either --dcos.toml-file or both --dcos.url and --dcos.acs-token")

	ff = &fromFlags{token: "bar", tomlFile: "baz"}
	assert.ErrorContains(t, ff.Validate(), "Must specify either --dcos.toml-file or both --dcos.url and --dcos.acs-token")

	ff = &fromFlags{url: "foo", tomlFile: "baz"}
	assert.ErrorContains(t, ff.Validate(), "Must specify either --dcos.toml-file or both --dcos.url and --dcos.acs-token")
}

func TestValidateBadTimeout(t *testing.T) {
	ff := &fromFlags{url: "foo", token: "bar", timeout: 5 * time.Millisecond}
	assert.ErrorContains(t, ff.Validate(), `--dcos.request-timeout must be >= 1, was "5ms"`)
}

func TestMakeFlags(t *testing.T) {
	ff := &fromFlags{url: "foo", token: "bar", timeout: DefaultRequestTimeout, insecure: true}
	cfg, err := ff.Make()

	assert.Nil(t, err)
	assert.Equal(t, cfg.URL, ff.url)
	assert.Equal(t, cfg.ACSToken, ff.token)
	assert.Equal(t, cfg.RequestTimeout, ff.timeout)
	assert.Equal(t, cfg.Insecure, ff.insecure)
}

func TestMakeTomlFile(t *testing.T) {
	data := `[core]
dcos_url = "http://dcos.snakeoil.mesosphere.com"
ssl_verify = false
timeout = 2
dcos_acs_token = "yoyodyne"`

	file, cleanup := tempfile.Write(t, data)
	defer cleanup()

	ff := &fromFlags{tomlFile: file}
	cfg, err := ff.Make()

	assert.Nil(t, err)
	assert.Equal(t, cfg.URL, "http://dcos.snakeoil.mesosphere.com")
	assert.Equal(t, cfg.ACSToken, "yoyodyne")
	assert.Equal(t, cfg.RequestTimeout, 2*time.Second)
	assert.Equal(t, cfg.Insecure, true)
}

func TestMakeBadTomlFile(t *testing.T) {
	file, cleanup := tempfile.Write(t, "bunk")
	defer cleanup()

	ff := &fromFlags{tomlFile: file}
	cfg, err := ff.Make()

	assert.NonNil(t, err)
	assert.Equal(t, cfg, DCOSConfig{})
}
