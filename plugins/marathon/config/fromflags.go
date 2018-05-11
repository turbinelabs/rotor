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
	"time"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
)

const (
	// DefaultRequestTimeout is the default value for the --dcos.request-timeout flag
	DefaultRequestTimeout = 5 * time.Second

	flagRequirementError = "Must specify either --dcos.toml-file or both --dcos.url and --dcos.acs-token"
)

// FromFlags represents the command-line flags specifying configuration of a
// DCOSConfig
type FromFlags interface {
	Validate() error
	Make() (DCOSConfig, error)
}

// NewFromFlags produces a FromFlags, adding necessary flags to the provided
// flag.FlagSet
func NewFromFlags(fs tbnflag.FlagSet) FromFlags {
	ff := &fromFlags{}

	scoped := fs.Scope("dcos", "")

	scoped.StringVar(
		&ff.url,
		"url",
		"",
		"The the public master IP of your DC/OS installation. Required unless --dcos.toml-file is set. Cannot be combined with --dcos.toml-file.",
	)

	scoped.BoolVar(
		&ff.insecure,
		"insecure",
		false,
		"If true, do not verify DC/OS SSL certificates",
	)

	scoped.StringVar(
		&ff.token,
		"acs-token",
		"",
		"The ACS Token for authenticating DC/OS requests. Obtained by logging into the DC/OS CLI, and then invoking \"dcos config show core.dcos_acs_token\". Required unless --dcos.toml-file is set. Cannot be combined with --dcos.toml-file.",
	)

	scoped.DurationVar(
		&ff.timeout,
		"request-timeout",
		DefaultRequestTimeout,
		"The timeout for DC/OS requests.",
	)

	scoped.StringVar(
		&ff.tomlFile,
		"toml-file",
		"",
		"The path to a DC/OS CLI dcos.toml configuration file. Required unless --dcos.url and --dcos.acs-token are set. Cannot be combined with --dcos.url or --dcos.acs-token.",
	)

	return ff
}

type fromFlags struct {
	url      string
	insecure bool
	timeout  time.Duration
	token    string
	tomlFile string
}

func (ff *fromFlags) Validate() error {
	if ff.tomlFile == "" {
		if ff.url == "" || ff.token == "" {
			return errors.New(flagRequirementError)
		}
		if ff.timeout < 1*time.Second {
			return fmt.Errorf("--dcos.request-timeout must be >= 1, was %q", ff.timeout)
		}
	} else {
		if ff.url != "" || ff.token != "" {
			return errors.New(flagRequirementError)
		}
	}
	return nil
}

func (ff *fromFlags) Make() (DCOSConfig, error) {
	if ff.tomlFile == "" {
		return DCOSConfig{
			URL:            ff.url,
			Insecure:       ff.insecure,
			RequestTimeout: ff.timeout,
			ACSToken:       ff.token,
		}, nil
	}

	return NewFromTOMLFile(ff.tomlFile)
}
