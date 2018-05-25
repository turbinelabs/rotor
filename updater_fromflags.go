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

package rotor

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"errors"
	"log"
	"os"
	"time"

	apiclient "github.com/turbinelabs/api/client"
	apiflags "github.com/turbinelabs/api/client/flags"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/executor"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
)

const clientApp = apiclient.App("github.com/turbinelabs/rotor")

// UpdaterFromFlags produces a fully-configured updater.Updater. It differs from
// updater.FromFlags in that it will also configure the API and potentially an
// xDS server.
type UpdaterFromFlags interface {
	Validate() error
	ValidateXDSOnly() error
	Make() (updater.Updater, error)
	MakeXDS() (adapter.XDS, error)
}

// NewUpdaterFromFlags installs an UpdaterFromFlags into the given FlagSet
func NewUpdaterFromFlags(flagset tbnflag.FlagSet) UpdaterFromFlags {
	apiFlagset := flagset.Scope("api", "rotor")

	apiConfigFromFlags := apiflags.NewAPIConfigFromFlags(
		apiFlagset,
		apiflags.APIConfigSetAPIAuthKeyFromFlags(
			apiflags.NewAPIAuthKeyFromFlags(
				apiFlagset,
				apiflags.APIAuthKeyFlagsOptional(),
			),
		),
	)

	zoneFromFlags := apiflags.NewZoneFromFlags(apiFlagset, apiflags.ZoneFromFlagsNameOptional())

	statsFlagset := flagset.Scope("stats", "stats")
	executorFromFlags :=
		executor.NewFromFlagsWithDefaults(
			statsFlagset.Scope("exec", "API request executor"),
			executor.FromFlagsDefaults{
				AttemptTimeout: 1 * time.Second,
				Timeout:        10 * time.Second,
			},
		)

	// Use a separate set of flags for stats API host and related flags but share the
	// API key flag.
	statsAPIConfigFromFlags := apiflags.NewAPIConfigFromFlags(
		statsFlagset.Scope("api", "stats API"),
		apiflags.APIConfigSetAPIAuthKeyFromFlags(apiConfigFromFlags.APIAuthKeyFromFlags()),
	)

	statsClientFromFlags := apiflags.NewStatsClientFromFlags(
		clientApp,
		statsFlagset,
		apiflags.StatsClientWithAPIConfigFromFlags(statsAPIConfigFromFlags),
		apiflags.StatsClientWithExecutorFromFlags(executorFromFlags),
	)

	statsFromFlags := stats.NewFromFlags(
		statsFlagset,
		stats.EnableAPIStatsBackend(),
		stats.DefaultBackends("api"),
		stats.APIStatsOptions(
			stats.AllowEmptyAPIKey(),
			stats.SetStatsClientFromFlags(statsClientFromFlags),
			stats.SetZoneFromFlags(zoneFromFlags),
			stats.SetLogger(log.New(os.Stderr, "stats: ", log.LstdFlags)),
		),
	)

	xdsFlagset := flagset.Scope("xds", "xDS server")

	xdsFromFlags := adapter.NewXDSFromFlags(xdsFlagset, statsFromFlags)

	ff := &updaterFromFlags{
		apiConfigFromFlags: apiConfigFromFlags,
		apiClientFromFlags: apiflags.NewClientFromFlagsWithSharedAPIConfig(
			clientApp,
			apiFlagset,
			apiConfigFromFlags,
		),
		zoneFromFlags:    zoneFromFlags,
		updaterFromFlags: updater.NewFromFlags(flagset),
		xdsFromFlags:     xdsFromFlags,
		pollerFromFlags:  poller.NewFromFlags(xdsFlagset),
		statsFromFlags:   statsFromFlags,
	}

	xdsFlagset.BoolVar(
		&ff.disableXDS,
		"disabled",
		false,
		"Disables the xDS listener.",
	)

	xdsFlagset.IntVar(
		&ff.standalonePort,
		"standalone-port",
		80,
		"The port on which Envoys consuming the standalone xDS server should listen. Ignored if --api.key is specified.",
	)

	xdsFlagset.StringVar(
		&ff.standaloneProxyName,
		"standalone-cluster",
		adapter.DefaultCluster,
		"The name of the cluster for the Envoys consuming the standalone xDS server. Should match the --service-cluster flag for the envoy binary, or the ENVOY_NODE_CLUSTER value for the envoy-simple Docker image.",
	)

	xdsFlagset.StringVar(
		&ff.standaloneZoneName,
		"standalone-zone",
		adapter.DefaultZone,
		"The name of the zone for the Envoys consuming the standalone xDS server. Should match the --service-zone flag for the envoy binary, or the ENVOY_NODE_ZONE value for the envoy-simple Docker image.",
	)

	ff.startXDS = ff.asyncStartXDS
	ff.pollLoop = asyncPollLoop

	return ff
}

type updaterFromFlags struct {
	disableXDS          bool
	apiConfigFromFlags  apiflags.APIConfigFromFlags
	apiClientFromFlags  apiflags.ClientFromFlags
	zoneFromFlags       apiflags.ZoneFromFlags
	updaterFromFlags    updater.FromFlags
	xdsFromFlags        adapter.XDSFromFlags
	pollerFromFlags     poller.FromFlags
	statsFromFlags      stats.FromFlags
	standalonePort      int
	standaloneProxyName string
	standaloneZoneName  string
	startXDS            func(adapter.XDS)
	pollLoop            func(poller.Poller)
}

func (ff *updaterFromFlags) Validate() error {
	return ff.validate(false)
}

func (ff *updaterFromFlags) ValidateXDSOnly() error {
	return ff.validate(true)
}

func (ff *updaterFromFlags) validate(xdsOnly bool) error {
	if err := ff.updaterFromFlags.Validate(); err != nil {
		return err
	}

	if err := ff.pollerFromFlags.Validate(); err != nil {
		return err
	}

	if ff.apiConfigFromFlags.APIKey() == "" {
		if xdsOnly {
			if ff.disableXDS {
				return errors.New("xDS is disabled, but xds-only was requested")
			}

			return errors.New("--api.key must be specified for xds-only")
		}

		return nil
	}

	if !xdsOnly && ff.zoneFromFlags.Name() == "" {
		return errors.New("--api.zone-name must be specified if --api.key is specified")
	}

	return ff.apiClientFromFlags.Validate()
}

func (ff *updaterFromFlags) syncStartXDS(x adapter.XDS) {
	x.Run()
}

func (ff *updaterFromFlags) asyncStartXDS(x adapter.XDS) {
	go ff.syncStartXDS(x)
}

func syncPollLoop(p poller.Poller) {
	p.PollLoop()
}

func asyncPollLoop(p poller.Poller) {
	go syncPollLoop(p)
}

func (ff *updaterFromFlags) mkSvcAndXDS() (service.All, adapter.XDS, error) {
	svc, err := ff.apiClientFromFlags.Make()
	if err != nil {
		return nil, nil, err
	}

	if ff.disableXDS {
		return svc, nil, nil
	}

	statsClient, err := ff.statsFromFlags.Make()
	if err != nil {
		return nil, nil, err
	}

	statsClient.AddTags(stats.NewKVTag(stats.ProxyVersionTag, constants.TbnPublicVersion))

	registrar := poller.NewRegistrar(svc)
	xds, err := ff.xdsFromFlags.Make(registrar)
	if err != nil {
		return nil, nil, err
	}

	poll := ff.pollerFromFlags.Make(svc, xds, registrar, statsClient)
	ff.pollLoop(poll)

	return svc, xds, nil

}

func (ff *updaterFromFlags) Make() (updater.Updater, error) {
	var (
		up  updater.Updater
		xds adapter.XDS
	)

	if ff.apiConfigFromFlags.APIKey() == "" {
		if ff.disableXDS {
			return nil, errors.New("no --api.key specified; " +
				"cannot use standalone mode because xDS is disabled")
		}

		console.Info().Printf(
			"No --api.key specified. "+
				"Using standalone mode: Envoys will be configured to serve on port %d, "+
				"for Envoy cluster %q in zone %q.",
			ff.standalonePort,
			ff.standaloneProxyName,
			ff.standaloneZoneName,
		)
		newUpdater, registrar := ff.updaterFromFlags.MakeStandalone(
			ff.standalonePort,
			ff.standaloneProxyName,
			ff.standaloneZoneName,
		)

		var err error
		xds, err = ff.xdsFromFlags.Make(registrar)
		if err != nil {
			return nil, err
		}
		up = newUpdater(xds)
	} else {
		var (
			svc service.All
			err error
		)

		svc, xds, err = ff.mkSvcAndXDS()
		if err != nil {
			return nil, err
		}

		zone, err := ff.zoneFromFlags.Ref().Get(svc)
		if err != nil {
			return nil, err
		}

		up = ff.updaterFromFlags.Make(svc, zone)
	}

	if ff.disableXDS {
		return up, nil
	}

	ff.startXDS(xds)

	return &updaterWithXDS{Updater: up, xds: xds}, nil
}

func (ff *updaterFromFlags) MakeXDS() (adapter.XDS, error) {
	if ff.disableXDS {
		return nil, errors.New("xDS is disabled")
	}

	_, xds, err := ff.mkSvcAndXDS()
	if err != nil {
		return nil, err
	}

	return xds, nil
}
