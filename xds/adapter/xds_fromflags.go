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

package adapter

import (
	"time"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
)

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

const (
	defaultAddr           = ":50000"
	defaultDefaultTimeout = 60 * time.Second
)

// XDSFromFlags configures and validates an XDS
type XDSFromFlags interface {
	// Make creates an XDS from the given poller.Registrar and command line flags.
	Make(registrar poller.Registrar) (XDS, error)
	// Validate verifies that all flags are correctly specified
	Validate() error
}

// NewXDSFromFlags installs a FromFlags in the given FlagSet
func NewXDSFromFlags(
	flags tbnflag.FlagSet,
	statsFromFlags stats.FromFlags,
) XDSFromFlags {
	ff := &xdsFromFlags{
		statsFromFlags:     statsFromFlags,
		resourcesFromFlags: newResourcesFromFlags(flags.Scope("static-resources", "static resources")),
	}

	flags.HostPortVar(
		&ff.addr,
		"addr",
		tbnflag.NewHostPort(defaultAddr),
		"The address on which to serve the envoy API server.",
	)

	flags.StringVar(
		&ff.caFile,
		"ca-file",
		"",
		"Path to a file (on the Envoy host's file system) containing CA certificates for TLS.",
	)

	flags.IntVar(
		&ff.grpcLogTopN,
		"grpc-log-top",
		0,
		"When gRPC logging is enabled and this value is greater than 1, logs of non-success Envoy "+
			"responses are tracked and periodically reported. This flag controls how many unique "+
			"response code & request path combinations are tracked. When the number of tracked "+
			"combinations in the reporting period is exceeded, uncommon paths are evicted.",
	)

	flags.DurationVar(
		&ff.grpcLogTopInterval,
		"grpc-log-top-interval",
		5*time.Minute,
		"See the grpc-log-top flag. Controls the interval at which top logs are generated.",
	)

	flags.DurationVar(
		&ff.defaultTimeout,
		"default-timeout",
		defaultDefaultTimeout,
		"The default request timeout, if none is specified in the RetryPolicy for a Route",
	)

	flags.BoolVar(
		&ff.resolveDNS,
		"resolve-dns",
		false,
		"If true, resolve EDS hostnames to IP addresses.",
	)

	return ff
}

type xdsFromFlags struct {
	addr               tbnflag.HostPort
	caFile             string
	grpcLogTopN        int
	grpcLogTopInterval time.Duration
	statsFromFlags     stats.FromFlags
	defaultTimeout     time.Duration
	resolveDNS         bool
	resourcesFromFlags resourcesFromFlags
}

func (ff *xdsFromFlags) Make(registrar poller.Registrar) (XDS, error) {
	stats, err := ff.statsFromFlags.Make()
	if err != nil {
		return nil, err
	}

	provider, err := ff.resourcesFromFlags.Make()
	if err != nil {
		return nil, err
	}

	return NewXDS(
		ff.addr.Addr(),
		registrar,
		ff.caFile,
		ff.defaultTimeout,
		ff.resolveDNS,
		stats,
		WithTopResponseLog(ff.grpcLogTopN, ff.grpcLogTopInterval),
		withStaticResources(provider),
	)
}

func (ff *xdsFromFlags) Validate() error {
	return ff.resourcesFromFlags.Validate()
}
