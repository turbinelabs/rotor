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

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
)

var lastLoopNotifier chan<- os.Signal

// Loop is a utility function for Rotor plugins that periodically polls for
// updates. It invokes the get function at the Updater's minimum update interval
// (since more frequent updates would be ignored). The result of get is passed to
// Updater.Replace, unless there is an error, in which case the error is logged.
// Loop installs a signal handler for SIGINT and SIGTERM (viaa SignalNotifier). If it
// receives either signal, it exits the polling loop after closing the Updater.
func Loop(updater Updater, get func() ([]api.Cluster, error)) {
	notifier := SignalNotifier()
	lastLoopNotifier = notifier

	looper := &updateLooper{
		time:     tbntime.NewSource(),
		signalCh: notifier,
	}
	looper.run(updater, get)
}

// StopLoop stops a running Loop invocation by simulating a signal. This function is
// intended for use in tests only. StopLoop assumes only one Loop is running in a
// given process, and therefore only the most recently started Loop will receive the
// simulated signal.
func StopLoop() {
	lastLoopNotifier <- syscall.SIGINT
	lastLoopNotifier = nil
}

// SignalNotifier creates a chan os.Signal that will receive the SIGINT and SIGTERM
// signals if the current process receives them. If Loop is inadequate for your Rotor
// plugin, this function can be used to provide the same exit-on-signal behavior. See
// github.com/
func SignalNotifier() chan os.Signal {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	return signalCh
}

type updateLooper struct {
	time     tbntime.Source
	signalCh chan os.Signal
}

func (looper *updateLooper) run(updater Updater, get func() ([]api.Cluster, error)) {
	defer updater.Close()
	defer signal.Stop(looper.signalCh)

	timer := looper.time.NewTimer(0)
	for {
		select {
		case <-timer.C():
			console.Debug().Println("polling clusters")
			clusters, err := get()
			if err == nil {
				updater.Replace(clusters)
			} else {
				console.Error().Printf("update error: %s", err.Error())
			}

			timer.Reset(updater.Delay())

		case signal := <-looper.signalCh:
			console.Info().Printf("%s: exiting", signal.String())
			return
		}
	}
}
