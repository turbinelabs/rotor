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

package v1

import (
	"net/http"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/collector"
	"github.com/turbinelabs/rotor/xds/collector/v1"
)

type resolverFactory = func(clusterManager) (collector.ClusterResolver, error)

func newResolverFactory(
	client func(string) (*http.Response, error),
	selector func(api.Instances) api.Instance,
) resolverFactory {
	return func(cm clusterManager) (collector.ClusterResolver, error) {
		if len(cm.Sds.Cluster.Hosts) < 1 {
			return nil, nil
		}

		sdsInstances := api.Instances{}
		for _, h := range cm.Sds.Cluster.Hosts {
			i, err := mkInstance(h.URL)
			if err != nil {
				return nil, err
			}

			sdsInstances = append(sdsInstances, i)
		}

		i := selector(sdsInstances)
		return v1.NewClusterResolver(i.Host, i.Port, client), nil
	}
}
