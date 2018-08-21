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
	"sync"
	"time"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
)

// Registrar allows consumers to declare what proxies they're interesting in
// getting *Objects for.
type Registrar interface {
	// Register a proxy. ifFirst is called synchronously if the proxy was not
	// registered by anyone else before the call. ifFirst is called within the
	// critical section; as such calling Register or Deregister from within
	// ifFirst is forbidden.
	Register(pRef service.ProxyRef, ifFirst func(service.All, api.Proxy)) error

	// Deregister a proxy. ifLast is called synchronously if the proxy is no longer
	// registered by anyone after the call. ifLast is called within the
	// critical section; as such calling Register or Deregister from within
	// ifLast is forbidden.
	Deregister(pRef service.ProxyRef, ifLast func()) error

	// Refs returns the list of currently registered proxies
	Refs() []service.ProxyRef
}

// NewRegistrar makes a new Registrar
func NewRegistrar(svc service.All) Registrar {
	return &registrar{
		svc:      svc,
		refCount: map[string]uint{},
		refs:     map[string]service.ProxyRef{},
	}
}

// NewNopRegistrar returns a Registrar that does nothing, and returns the given
// proxy refs
func NewNopRegistrar() Registrar {
	return nopRegistar{}
}

// NewDelayedRegistrar returns a Registrar that delays Deregistration by the
// specified interval. Deregeistration failures are logged to console.Error but
// otherwise ignored.
func NewDelayedRegistrar(r Registrar, delay time.Duration) Registrar {
	return delayedRegistrar{
		Registrar: r,
		delay:     func(f func()) { tbntime.AfterFunc(delay, f) },
	}
}

type nopRegistar struct{ refs []service.ProxyRef }

func (nopRegistar) Register(_ service.ProxyRef, _ func(service.All, api.Proxy)) error {
	return nil
}
func (nopRegistar) Deregister(_ service.ProxyRef, _ func()) error { return nil }
func (r nopRegistar) Refs() []service.ProxyRef                    { return nil }

type registrar struct {
	sync.RWMutex
	svc      service.All
	refCount map[string]uint
	refs     map[string]service.ProxyRef
}

func (r *registrar) Register(pRef service.ProxyRef, ifFirst func(service.All, api.Proxy)) error {
	// better to blow up here if it's a fake proxy than in the poll loop
	proxy, err := pRef.Get(r.svc)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()

	k := pRef.MapKey()

	r.refCount[k]++
	if r.refs[k] == nil {
		r.refs[k] = pRef
		// note that this is a synchronous call; this must complete before the next
		// registration call is serviced
		if ifFirst != nil {
			ifFirst(r.svc, proxy)
		}
	}
	return nil
}

func (r *registrar) Deregister(pRef service.ProxyRef, ifLast func()) error {
	r.Lock()
	defer r.Unlock()

	// pRef.Get isn't called here because either (1) the pRef has been resolved
	// when it was registered or (2) it's not registered. Either way there is no
	// need to validate it.

	k := pRef.MapKey()

	// blow up if we're deregistering a key we don't know about
	if r.refCount[k] == 0 {
		return fmt.Errorf("deregister attempt on unregistered proxy: %q", k)
	}

	// clean up ref if necessary
	r.refCount[k]--
	if r.refCount[k] == 0 {
		delete(r.refs, k)
		delete(r.refCount, k)
		// note that this is a synchronous call; this must complete before the next
		// deregistration call is serviced
		if ifLast != nil {
			ifLast()
		}
	}

	return nil
}

func (r *registrar) Refs() []service.ProxyRef {
	refs := make([]service.ProxyRef, 0, len(r.refs))
	r.RLock()
	defer r.RUnlock()

	for _, ref := range r.refs {
		refs = append(refs, ref)
	}

	return refs
}

type delayedRegistrar struct {
	Registrar
	delay func(func())
}

func (r delayedRegistrar) Deregister(pRef service.ProxyRef, ifLast func()) error {
	k := pRef.MapKey()
	r.delay(func() {
		if err := r.Registrar.Deregister(pRef, ifLast); err != nil {
			console.Error().Printf("Error deregistering node(%s): %s", k, err)
		}
	})
	return nil
}
