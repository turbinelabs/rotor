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

package updater

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"errors"
	"time"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor/differ"
	"github.com/turbinelabs/rotor/xds/poller"
)

// FromFlags represents command-line flags specifying configuration of an Updater.
type FromFlags interface {
	Validate() error
	Make(service.All, api.Zone) Updater
	MakeStandalone(port int) (func(poller.Consumer) Updater, poller.Registrar)
}

const defaultDelaySeconds = 30

// FromFlagOptions lets you add various options to NewFromFlags
type FromFlagOptions func(*fromFlags) *fromFlags

// SkipMinDelay skips enforcement of a minimum delay
func SkipMinDelay() FromFlagOptions {
	return func(ff *fromFlags) *fromFlags {
		ff.skipMinDelay = true
		return ff
	}
}

// WithDefaultDelay allows specification of a flag-configured delay
func WithDefaultDelay(i uint) FromFlagOptions {
	return func(ff *fromFlags) *fromFlags {
		ff.defaultDelay = int(i)
		return ff
	}
}

// NewFromFlags installs a FromFlags in the given FlagSet
func NewFromFlags(
	flagset tbnflag.FlagSet,
	opts ...FromFlagOptions,
) FromFlags {
	ff := &fromFlags{
		flagset:      flagset,
		diffOpts:     differ.DiffOptsFromFlags(flagset.Scope("diff.", "")),
		defaultDelay: defaultDelaySeconds,
	}

	for _, fn := range opts {
		ff = fn(ff)
	}

	flagset.DurationVar(
		&ff.delay,
		"delay",
		time.Duration(ff.defaultDelay)*time.Second,
		"Sets the minimum time between API updates. If the discovery data changes more frequently than this duration, updates are delayed to maintain the minimum time.")

	return ff
}

type fromFlags struct {
	flagset      tbnflag.FlagSet
	diffOpts     *differ.DiffOpts
	delay        time.Duration
	defaultDelay int
	skipMinDelay bool
}

func (ff *fromFlags) Validate() error {
	if !ff.skipMinDelay {
		if ff.delay < 1*time.Second {
			return errors.New("delay may not be less than 1 second")
		}
	}
	return nil
}

func (ff *fromFlags) Make(svc service.All, zone api.Zone) Updater {
	return New(
		differ.New(svc.Cluster(), zone.GetZoneKey()),
		ff.delay,
		*ff.diffOpts,
		zone.Name,
	)
}

func (ff *fromFlags) MakeStandalone(port int) (func(poller.Consumer) Updater, poller.Registrar) {
	newDiffer, reg := differ.NewStandalone(port)
	return func(consumer poller.Consumer) Updater {
		return New(newDiffer(consumer), ff.delay, *ff.diffOpts, "")
	}, reg
}
