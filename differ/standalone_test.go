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
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/matcher"
)

func TestStandaloneDifferDiff(t *testing.T) {
	sd := standaloneDiffer{}

	clusters := api.Clusters{
		{Name: "C1"},
		{Name: "C2"},
		{Name: "C3"},
	}

	want := []Diff{
		NewDiffCreate(api.Cluster{Name: "C1"}),
		NewDiffCreate(api.Cluster{Name: "C2"}),
		NewDiffCreate(api.Cluster{Name: "C3"}),
	}

	got, err := sd.Diff(clusters, DiffOpts{})

	assert.DeepEqual(t, got, want)
	assert.Nil(t, err)
}

func TestStandaloneDifferPatch(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	consumer := poller.NewMockConsumer(ctrl)

	sd := standaloneDiffer{port: 1234, consumer: consumer}

	diffs := []Diff{
		NewDiffCreate(api.Cluster{Name: "C1"}),
		NewDiffCreate(api.Cluster{Name: "C2"}),
		NewDiffCreate(api.Cluster{Name: "C3"}),
	}

	want := &poller.Objects{
		Clusters: api.Clusters{
			{ClusterKey: "C1", Name: "C1"},
			{ClusterKey: "C2", Name: "C2"},
			{ClusterKey: "C3", Name: "C3"},
		},
		Domains: api.Domains{
			{DomainKey: "C1:1234", Name: "C1", Port: 1234},
			{DomainKey: "C2:1234", Name: "C2", Port: 1234},
			{DomainKey: "C3:1234", Name: "C3", Port: 1234},
		},
		Routes: api.Routes{
			{
				RouteKey:       "C1:1234/",
				DomainKey:      "C1:1234",
				Path:           "/",
				SharedRulesKey: "C1",
			},
			{
				RouteKey:       "C2:1234/",
				DomainKey:      "C2:1234",
				Path:           "/",
				SharedRulesKey: "C2",
			},
			{
				RouteKey:       "C3:1234/",
				DomainKey:      "C3:1234",
				Path:           "/",
				SharedRulesKey: "C3",
			},
		},
		SharedRules: api.SharedRulesSlice{
			{
				SharedRulesKey: "C1",
				Default: api.AllConstraints{
					Light: api.ClusterConstraints{
						{
							ClusterKey: "C1",
							Weight:     1,
						},
					},
				},
			},
			{
				SharedRulesKey: "C2",
				Default: api.AllConstraints{
					Light: api.ClusterConstraints{
						{
							ClusterKey: "C2",
							Weight:     1,
						},
					},
				},
			},
			{
				SharedRulesKey: "C3",
				Default: api.AllConstraints{
					Light: api.ClusterConstraints{
						{
							ClusterKey: "C3",
							Weight:     1,
						},
					},
				},
			},
		},
		Proxy: api.Proxy{
			ZoneKey:    "default-zone",
			ProxyKey:   "default-cluster",
			Name:       "default-cluster",
			DomainKeys: []api.DomainKey{"C1:1234", "C2:1234", "C3:1234"},
		},
		Zone: api.Zone{ZoneKey: "default-zone", Name: "default-zone"},
	}

	captor := matcher.CaptureType(reflect.TypeOf(want))
	consumer.EXPECT().Consume(captor)
	err := sd.Patch(diffs)
	assert.DeepEqual(t, captor.V.(*poller.Objects), want)
	assert.Nil(t, err)
}

func TestStandaloneDifferPatchNonCreate(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	consumer := poller.NewMockConsumer(ctrl)

	sd := standaloneDiffer{port: 1234, consumer: consumer}

	err := sd.Patch([]Diff{NewDiffDelete("", api.Checksum{})})
	assert.NonNil(t, err)
}
