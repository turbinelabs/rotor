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

// Package poller provides a interfaces for polling and consuming proxy
// configuration objects from the Turbine Labs API
package poller

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"errors"
	"time"

	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/stats"
)

// Consumer receives the *Objects retrieved by an iteration of a Poller.
type Consumer interface {
	Consume(*Objects) error
}

// Poller polls for the configuration objects of a Proxy.
type Poller interface {
	// Poll will perform a single iteration that will produce objects that are
	// pushed to a Consumer. It may update internal state during the iteration.
	Poll() error

	// PollLoop polls for changes to the configuration objects of a Proxy in an
	// infinite loop, logging errors. Closing the Poller signals the loop to exit.
	PollLoop()

	// Close causes PollLoop to exit.
	Close() error
}

// New creates a Poller backed by the given service.All
func New(
	svc service.All,
	consumer Consumer,
	registrar Registrar,
	pollInterval time.Duration,
	stats stats.Stats,
) Poller {
	return &poller{
		svc:          svc,
		remote:       NewRemote(svc),
		consumer:     consumer,
		registrar:    registrar,
		pollInterval: pollInterval,
		stats:        stats,
		time:         tbntime.NewSource(),
		quitCh:       make(chan struct{}),
	}
}

type poller struct {
	svc          service.All
	remote       Remote
	consumer     Consumer
	registrar    Registrar
	pollInterval time.Duration
	stats        stats.Stats
	time         tbntime.Source
	quitCh       chan struct{}
}

func (p *poller) PollLoop() {
	// Create a timer and stop it such that it's guaranteed safe
	// for a future Reset.
	timer := p.time.NewTimer(time.Hour)
	defer timer.Stop()
	if !timer.Stop() {
		<-timer.C()
	}

	for {
		p.Poll()

		timer.Reset(p.pollInterval)
		select {
		case <-p.quitCh:
			console.Info().Println("api polling loop exit")
			return
		case <-timer.C():
		}
	}
}

// Poll polls the API for the registered proxy objects
func (p *poller) Poll() error {
	var firstErr error
	// TODO(https://github.com/turbinelabs/tbn/issues/4205)
	// pass all refs into Remote.Objects, for less redundant
	// query pattern.
	for _, pRef := range p.registrar.Refs() {
		name := pRef.Name()
		result := "success"
		if err := p.pollOne(pRef); err != nil {
			console.Error().Printf("api polling error for %s: %s", name, err)
			result = "error"
			if firstErr == nil {
				firstErr = err
			}
		}
		tags := []stats.Tag{
			stats.NewKVTag("result", result),
			stats.NewKVTag(stats.ProxyTag, name),
		}
		if zRef := pRef.ZoneRef(); zRef != nil {
			tags = append(tags, stats.NewKVTag(stats.ZoneTag, zRef.Name()))
		}
		p.stats.Count("poll", 1.0, tags...)
	}
	return firstErr
}

func (p *poller) pollOne(pRef service.ProxyRef) error {
	proxy, err := pRef.Get(p.svc)
	if err != nil {
		return err
	}

	objects, err := p.remote.Objects(proxy.ProxyKey)
	if err != nil {
		return err
	}

	if objects != nil {
		err := p.consumer.Consume(objects)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *poller) Close() error {
	select {
	case _, ok := <-p.quitCh:
		if !ok {
			return errors.New("already closed")
		}

	default:
	}

	close(p.quitCh)
	return nil
}
