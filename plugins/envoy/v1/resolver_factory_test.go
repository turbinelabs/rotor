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
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

const (
	goodSdsResponse = `
{
  "hosts": [
    {
      "ip_address": "127.0.0.1",
      "port": 9001
    },
    {
      "ip_address": "127.0.0.2",
      "port": 9002
    },
    {
      "ip_address": "127.0.0.3",
      "port": 9003
    }
  ]
}
`
)

var (
	expectedInstances = []api.Instance{
		{
			Host: "127.0.0.1",
			Port: 9001,
		},
		{
			Host: "127.0.0.2",
			Port: 9002,
		},
		{
			Host: "127.0.0.3",
			Port: 9003,
		},
	}
)

func constInstanceSelector(is api.Instances) api.Instance {
	return is[0]
}

func TestNewResolverFactoryBadClusterManager(t *testing.T) {
	factory := newResolverFactory(nil, constInstanceSelector)
	cr, err := factory(clusterManager{})

	assert.Nil(t, cr)
	assert.Nil(t, err)
}

func TestNewResolverFactoryBadInstance(t *testing.T) {
	factory := newResolverFactory(nil, constInstanceSelector)
	cr, err := factory(clusterManager{
		Sds: sds{
			Cluster: cluster{
				Hosts: []host{
					{
						URL: "thisisbad.com",
					},
				},
			},
		},
	})

	assert.Nil(t, cr)
	assert.NonNil(t, err)
}

func TestGoodSdsDefinitionEmptyHttpResponse(t *testing.T) {
	cm := clusterManager{
		Sds: sds{
			Cluster: cluster{
				Hosts: []host{
					{
						URL: "tcp://127.0.0.2:9001",
					},
					{
						URL: "tcp://127.0.0.3:9003",
					},
					{
						URL: "tcp://127.0.0.1:9000",
					},
				},
			},
		},
	}

	var actualDestinationURL string
	factory := newResolverFactory(
		func(url string) (*http.Response, error) {
			actualDestinationURL = url
			return &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
			}, nil
		},
		constInstanceSelector,
	)

	cr, err := factory(cm)
	assert.NonNil(t, cr)
	assert.Nil(t, err)

	instances, errs := cr("boop")
	assert.ArrayEqual(t, instances, []api.Instance{})
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, actualDestinationURL, "http://127.0.0.2:9001/v1/registration/boop")
}

func TestGoodSdsDefinitionGoodSdsResponse(t *testing.T) {
	cm := clusterManager{
		Sds: sds{Cluster: cluster{Hosts: []host{{URL: "tcp://127.0.0.1:9000"}}}},
	}

	var actualDestinationURL string
	factory := newResolverFactory(
		func(url string) (*http.Response, error) {
			actualDestinationURL = url
			return &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString(goodSdsResponse)),
			}, nil
		},
		constInstanceSelector,
	)

	cr, err := factory(cm)
	assert.NonNil(t, cr)
	assert.Nil(t, err)

	instances, errs := cr("boop")
	assert.HasSameElements(t, instances, expectedInstances)
	assert.Equal(t, len(errs), 0)
	assert.Equal(t, actualDestinationURL, "http://127.0.0.1:9000/v1/registration/boop")
}
