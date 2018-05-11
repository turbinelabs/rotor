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

package differ

import (
	"fmt"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/poller"
)

const (
	zoneName  = "default-zone"
	proxyName = "default-cluster"
)

var (
	standaloneZone = api.Zone{ZoneKey: zoneName, Name: zoneName}
)

func mkStandaloneProxy(dks ...api.DomainKey) api.Proxy {
	return api.Proxy{
		ZoneKey:    zoneName,
		ProxyKey:   proxyName,
		Name:       proxyName,
		DomainKeys: dks,
	}
}

// standaloneDiffer produces always produces Create diffs, and the Patch call
// takes those create diffs and creates a simple poller.Objects serving "/"
// on a specified port for each cluster, with the cluster host/port as the
// domain name.
type standaloneDiffer struct {
	port     int
	consumer poller.Consumer
}

// NewStandalone produces a function that will create a Differ from a
// poller.Consumer, and a Registrar suitable for injection into an xDS.
func NewStandalone(port int) (func(poller.Consumer) Differ, poller.Registrar) {
	return func(consumer poller.Consumer) Differ {
		return standaloneDiffer{port: port, consumer: consumer}
	}, poller.NewNopRegistrar()
}

func (d standaloneDiffer) Diff(proposed []api.Cluster, _ DiffOpts) ([]Diff, error) {
	diffs := make([]Diff, len(proposed), len(proposed))
	for i, c := range proposed {
		diffs[i] = NewDiffCreate(c)
	}
	return diffs, nil
}

func (d standaloneDiffer) Patch(diffs []Diff) error {
	objs := &poller.Objects{
		Clusters:    make(api.Clusters, len(diffs), len(diffs)),
		Domains:     make(api.Domains, len(diffs), len(diffs)),
		Routes:      make(api.Routes, len(diffs), len(diffs)),
		SharedRules: make(api.SharedRulesSlice, len(diffs), len(diffs)),
	}

	dks := make([]api.DomainKey, len(diffs), len(diffs))

	for i, diff := range diffs {
		switch t := diff.(type) {
		case *diffCreate:
			c := t.cluster

			ck := api.ClusterKey(c.Name)
			c.ClusterKey = ck

			dom := api.Domain{Name: c.Name, Port: d.port}
			dom.DomainKey = api.DomainKey(dom.Addr())
			dks[i] = dom.DomainKey

			srk := api.SharedRulesKey(c.Name)

			objs.Clusters[i] = c
			objs.Domains[i] = dom
			objs.Routes[i] = api.Route{
				RouteKey:       api.RouteKey(fmt.Sprintf("%s/", dom.DomainKey)),
				DomainKey:      dom.DomainKey,
				Path:           "/",
				SharedRulesKey: srk,
			}
			objs.SharedRules[i] = api.SharedRules{
				SharedRulesKey: srk,
				Default: api.AllConstraints{
					Light: api.ClusterConstraints{
						{
							ClusterKey: ck,
							Weight:     1,
						},
					},
				},
			}

		default:
			return fmt.Errorf("unexpected Diff type: %T", t)
		}
	}

	objs.Proxy = mkStandaloneProxy(dks...)
	objs.Zone = standaloneZone

	d.consumer.Consume(objs)

	return nil
}
