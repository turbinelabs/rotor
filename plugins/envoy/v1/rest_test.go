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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"
)

func TestRestRunnerInvalidAddr(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdater := updater.NewMockUpdater(ctrl)

	rr := restRunner{
		updaterFlags: mockUpdaterFlags,
		addr:         tbnflag.NewHostPort("not a domain:99"),
		sdsAddr:      tbnflag.NewHostPort("my-sds.com:9000"),
	}

	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFlags.EXPECT().Make().Return(mockUpdater, nil)

	cmdErr := rr.Run(RESTCmd(mockUpdaterFlags), nil)
	msg := "envoy-cds-v1: parse http://not a domain:99: invalid character \" \" in host name"
	assert.Equal(t, cmdErr.Message, msg)
}

func TestRestRunnerInvalidSdsAddr(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdater := updater.NewMockUpdater(ctrl)

	rr := restRunner{
		updaterFlags: mockUpdaterFlags,
		addr:         tbnflag.NewHostPort("my-cds.com:9900"),
		sdsAddr:      tbnflag.NewHostPort("not a domain:99"),
	}

	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFlags.EXPECT().Make().Return(mockUpdater, nil)

	cmdErr := rr.Run(RESTCmd(mockUpdaterFlags), nil)
	msg := "envoy-cds-v1: parse http://not a domain:99: invalid character \" \" in host name"
	assert.Equal(t, cmdErr.Message, msg)
}

func TestRestRunnerBadUpdater(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	rr := restRunner{
		updaterFlags: mockUpdaterFlags,
		addr:         tbnflag.NewHostPort("my-cds.com:9900"),
		sdsAddr:      tbnflag.NewHostPort("my-sds.com:9900"),
	}

	err := errors.New("boom")
	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFlags.EXPECT().Make().Return(nil, err)

	cmdErr := rr.Run(RESTCmd(mockUpdaterFlags), nil)
	assert.Equal(t, cmdErr.Message, fmt.Sprintf("envoy-cds-v1: %s", err))
}

func TestRestRunnerBadClustersNodes(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdater := updater.NewMockUpdater(ctrl)

	strings := tbnflag.Strings{Strings: []string{"nobueno::blerp"}, Delimiter: ","}
	rr := restRunner{
		updaterFlags:  mockUpdaterFlags,
		addr:          tbnflag.NewHostPort("my-cds.com:9900"),
		sdsAddr:       tbnflag.NewHostPort("my-sds.com:9900"),
		clustersNodes: strings,
	}

	mockUpdaterFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFlags.EXPECT().Make().Return(mockUpdater, nil)
	cmdErr := rr.Run(RESTCmd(mockUpdaterFlags), nil)
	em := "envoy-cds-v1: clustersNodes must be of the form \"<cluster>:<node>\" or " +
		"\"<cluster>\", but was nobueno::blerp"
	assert.Equal(t, cmdErr.Message, em)
}

func TestMkCdsRoutesWithNoEscapedCharacters(t *testing.T) {
	input := []string{"cluster:node", "cluster1:node1", "cluster4", "cluster2:node2", "cluster3"}
	paths, err := mkCdsRoutes(input)
	assert.Nil(t, err)
	assert.ArrayEqual(t, paths, []string{
		"/v1/clusters/cluster/node",
		"/v1/clusters/cluster1/node1",
		"/v1/clusters/cluster4",
		"/v1/clusters/cluster2/node2",
		"/v1/clusters/cluster3",
	})
}

func TestMkCdsRoutesWithEscapedCharacters(t *testing.T) {
	input := []string{"c!$#$:node", "c[usrfd]:node1"}
	paths, err := mkCdsRoutes(input)
	assert.Nil(t, err)
	assert.ArrayEqual(t, paths, []string{
		"/v1/clusters/c%21$%23$/node",
		"/v1/clusters/c%5Busrfd%5D/node1",
	})
}

func TestMkCdsRoutesErrorForBadArgs(t *testing.T) {
	inputs := [][]string{
		{":node"},
		{":"},
		{""},
	}

	for _, iSlice := range inputs {
		paths, err := mkCdsRoutes(iSlice)
		assert.NonNil(t, err)
		assert.Nil(t, paths)
	}
}

func TestRestCollectorGoodHttpResponse(t *testing.T) {
	expectedClusters := api.Clusters{
		{
			Name: "statsd",
			Instances: api.Instances{{
				Host: "127.0.0.1",
				Port: 8125,
			}},
		},
		{
			Name: "backhaul",
			Instances: api.Instances{{
				Host: "front-proxy.yourcompany.net",
				Port: 9400,
			}},
		},
		{
			Name: "lightstep_saas",
			Instances: api.Instances{{
				Host: "collector-grpc.lightstep.com",
				Port: 443,
			}},
		},
	}
	rc := &restCollector{
		clientFn: func(string) (*http.Response, error) {
			return &http.Response{
				Status:     "200 OK",
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
			}, nil
		},
		parserFn: func(io.Reader) ([]api.Cluster, error) {
			return expectedClusters, nil
		},
	}

	clusters, err := rc.getClusters("whatever")
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
}

func TestRestCollectorHttpError(t *testing.T) {
	var clientFnCnt, cdsParserCnt int
	rc := &restCollector{
		clientFn: func(string) (*http.Response, error) {
			clientFnCnt++
			return nil, errors.New("boom")
		},
		parserFn: func(io.Reader) ([]api.Cluster, error) {
			cdsParserCnt++
			return nil, nil
		},
	}

	clusters, err := rc.getClusters("whatever")
	assert.Nil(t, clusters)
	assert.NonNil(t, err)
	assert.Equal(t, clientFnCnt, 1)
	assert.Equal(t, cdsParserCnt, 0)
}

func TestRestCollectorNon200ResponseError(t *testing.T) {
	var clientFnCnt, cdsParserCnt int
	rc := &restCollector{
		clientFn: func(string) (*http.Response, error) {
			clientFnCnt++
			return &http.Response{
				Status:     "404 NOT FOUND",
				StatusCode: http.StatusNotFound,
			}, nil
		},
		parserFn: func(io.Reader) ([]api.Cluster, error) {
			cdsParserCnt++
			return nil, nil
		},
	}

	clusters, err := rc.getClusters("whatever")
	assert.Nil(t, clusters)
	assert.NonNil(t, err)
	assert.Equal(t, clientFnCnt, 1)
	assert.Equal(t, cdsParserCnt, 0)
}
