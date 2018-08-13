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
	"sort"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/poller"
)

// standaloneDiffer produces always produces Create diffs, and the Patch call
// takes those create diffs and creates a simple poller.Objects serving "/"
// on a specified port for each cluster, with the cluster host/port as the
// domain name.
type standaloneDiffer struct {
	port      int
	consumer  poller.Consumer
	proxyName string
	zoneName  string
}

// NewStandalone produces a function that will create a Differ from a
// poller.Consumer, and a Registrar suitable for injection into an xDS.
func NewStandalone(
	port int,
	proxyName,
	zoneName string,
) (func(poller.Consumer) Differ, poller.Registrar) {
	return func(consumer poller.Consumer) Differ {
		return standaloneDiffer{
			port:      port,
			consumer:  consumer,
			proxyName: proxyName,
			zoneName:  zoneName,
		}
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

			// protect against randomness in collection (this isn't a problem when
			// storing/retrieving clusters in the Houston API, because the metadata
			// comes back from the API in a reliable order, but here we don't have the
			// API to sanitize for us.)
			sort.Sort(api.InstancesByHostPort(c.Instances))
			for j := range c.Instances {
				sort.Sort(api.MetadataByKey(c.Instances[j].Metadata))
			}

			ck := api.ClusterKey(c.Name)
			c.ClusterKey = ck
			objs.Clusters[i] = c

		default:
			return fmt.Errorf("unexpected Diff type: %T", t)
		}
	}

	// And again with the stable ordering. The various other objects take their
	// order from the cluster order, so sort by name.
	sort.Sort(api.ClusterByName(objs.Clusters))

	for i, c := range objs.Clusters {
		dom := api.Domain{Name: c.Name, Port: d.port}
		dom.DomainKey = api.DomainKey(dom.Addr())
		dks[i] = dom.DomainKey

		srk := api.SharedRulesKey(c.Name)

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
						ClusterKey: c.ClusterKey,
						Weight:     1,
					},
				},
			},
		}
	}

	objs.Proxy = api.Proxy{
		ZoneKey:    api.ZoneKey(d.zoneName),
		ProxyKey:   api.ProxyKey(d.proxyName),
		Name:       d.proxyName,
		DomainKeys: dks,
	}

	objs.Zone = api.Zone{ZoneKey: api.ZoneKey(d.zoneName), Name: d.zoneName}

	d.consumer.Consume(objs)

	return nil
}
