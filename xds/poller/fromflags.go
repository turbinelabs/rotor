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

package poller

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"fmt"
	"time"

	"github.com/turbinelabs/api/service"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/stats"
)

const minPollInterval = 500 * time.Millisecond

// FromFlags produces a Poller from flag-based configuration
type FromFlags interface {
	Validate() error
	Make(service.All, Consumer, Registrar, stats.Stats) Poller
}

// NewFromFlags produces a FromFlags, and installs necessary
// configuration flags into the provided FlagSet.
func NewFromFlags(flagset tbnflag.FlagSet) FromFlags {
	ff := &fromFlags{}

	flagset.DurationVar(
		&ff.pollInterval,
		"interval",
		1*time.Second,
		fmt.Sprintf("The interval for polling the Turbine API. Minimium value is %s", minPollInterval),
	)

	return ff
}

type fromFlags struct {
	pollInterval time.Duration
}

func (ff *fromFlags) Validate() error {
	if ff.pollInterval < minPollInterval {
		return fmt.Errorf(
			"pollInterval must be greater than %s, was %s",
			minPollInterval,
			ff.pollInterval,
		)
	}

	return nil
}

func (ff *fromFlags) Make(
	svc service.All,
	co Consumer,
	re Registrar,
	statsClient stats.Stats,
) Poller {
	return New(svc, co, re, ff.pollInterval, statsClient)
}
