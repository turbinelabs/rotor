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
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DCOSConfig provides configuration data for a DCOS installation
type DCOSConfig struct {
	// URL returns the base URL of the DC/OS API.
	URL string
	// Insecure indicates whether TLS certificate checks should be ignored. This
	// is only reasonable in the case of test certificates.
	Insecure bool
	// RequestTimeout returns the amount of time to wait for requests to the DC/OS
	// API to complete.
	RequestTimeout time.Duration
	// ACSToken returns an autorization token for inclusion in requests to the DC/OS API.
	ACSToken string
}

// NewFromTOML Produces a new DCOSConfig from a TOML string
func NewFromTOML(tomlStr string) (DCOSConfig, error) {
	data := map[string]interface{}{}
	meta, err := toml.Decode(tomlStr, &data)
	if err != nil {
		return DCOSConfig{}, err
	}
	return newFromMeta(data, meta)
}

// NewFromTOMLFile Produces a new DCOSConfig from a TOML file
func NewFromTOMLFile(filename string) (DCOSConfig, error) {
	data := map[string]interface{}{}
	meta, err := toml.DecodeFile(filename, &data)
	if err != nil {
		return DCOSConfig{}, err
	}
	return newFromMeta(data, meta)
}

// dcos.toml is inconsistent in representation of bools as strings or bools.
// This is a big dance to be flexible about types.
func newFromMeta(data map[string]interface{}, meta toml.MetaData) (DCOSConfig, error) {
	conf := DCOSConfig{}

	missing := []string{}
	malformed := []string{}
	other := []string{}

	if meta.Type("core") != "Hash" {
		return DCOSConfig{}, errors.New("could not fild [core] section in dcos.toml")
	}
	core := data["core"].(map[string]interface{})

	switch meta.Type("core", "dcos_url") {
	case "String":
		uStr := core["dcos_url"].(string)
		u, err := url.Parse(uStr)
		if err != nil {
			malformed = append(malformed, fmt.Sprintf("core.dcos_url (%q)", uStr))
		} else {
			conf.URL = u.String()
		}
	case "":
		missing = append(missing, "core.dcos_url")
	default:
		malformed = append(malformed, fmt.Sprintf("core.dcos_url (%v)", core["dcos_url"]))
	}

	switch meta.Type("core", "ssl_verify") {
	case "String":
		sslStr := core["ssl_verify"].(string)
		sslVerify, err := strconv.ParseBool(sslStr)
		if err != nil {
			malformed = append(malformed, fmt.Sprintf("core.ssl_verify (%q)", sslStr))
		} else {
			conf.Insecure = !sslVerify
		}
	case "Bool":
		conf.Insecure = !core["ssl_verify"].(bool)
	case "":
		// optional, has a sane default
	default:
		malformed = append(malformed, fmt.Sprintf("core.ssl_verify (%v)", core["ssl_verify"]))
	}

	var (
		durInt    int64
		durIntSet bool
	)

	switch meta.Type("core", "timeout") {
	case "String":
		durStr := core["timeout"].(string)
		var err error
		durInt, err = strconv.ParseInt(durStr, 10, 64)
		if err != nil {
			malformed = append(malformed, fmt.Sprintf("core.timeout (%q)", durStr))
		} else {
			durIntSet = true
		}
	case "Integer":
		durInt = core["timeout"].(int64)
		durIntSet = true
	case "":
		// optional
		conf.RequestTimeout = DefaultRequestTimeout
	default:
		malformed = append(malformed, fmt.Sprintf("core.timeout (%v)", core["timeout"]))
	}

	if durIntSet {
		if durInt < 1 {
			other = append(other, "core.timeout must be >= 1")
		} else {
			conf.RequestTimeout = time.Duration(durInt) * time.Second
		}
	}

	switch meta.Type("core", "dcos_acs_token") {
	case "String":
		conf.ACSToken = core["dcos_acs_token"].(string)
	case "":
		missing = append(missing, "core.dcos_acs_token")
	default:
		malformed = append(malformed, fmt.Sprintf("core.dcos_acs_token (%v)", core["dcos_acs_token"]))
	}

	var errStr string
	if len(missing) != 0 {
		errStr += "the following fields were missing from dcos.toml: "
		errStr += strings.Join(missing, ", ")
	}
	if len(malformed) != 0 {
		if errStr != "" {
			errStr += "; "
		}
		errStr += "the following fields were malformed from dcos.toml: "
		errStr += strings.Join(malformed, ", ")
	}
	if len(other) != 0 {
		if errStr != "" {
			errStr += "; "
		}
		errStr += strings.Join(other, ", ")
	}

	if errStr != "" {
		return DCOSConfig{}, errors.New(errStr)
	}
	return conf, nil
}
