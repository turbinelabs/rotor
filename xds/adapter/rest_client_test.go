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

package adapter

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	"github.com/turbinelabs/test/assert"
)

func TestMarshallerErrorReturnsError(t *testing.T) {
	rc := &restDiscoveryService{
		marshaller: func(*envoyapi.DiscoveryRequest) (io.Reader, error) {
			return nil, errors.New("boom")
		},
	}

	resp, err := rc.Fetch(&envoyapi.DiscoveryRequest{})
	assert.Nil(t, resp)
	assert.NonNil(t, err)
}

func TestPostErrorReturnsError(t *testing.T) {
	rc := &restDiscoveryService{
		marshaller: func(*envoyapi.DiscoveryRequest) (io.Reader, error) {
			return bytes.NewBufferString("{}"), nil
		},
		httpPost: func(io.Reader) (*http.Response, error) {
			return nil, errors.New("boom")
		},
	}

	resp, err := rc.Fetch(&envoyapi.DiscoveryRequest{})
	assert.Nil(t, resp)
	assert.NonNil(t, err)
}

func TestNon200ResonseReturnsError(t *testing.T) {
	rc := &restDiscoveryService{
		marshaller: func(*envoyapi.DiscoveryRequest) (io.Reader, error) {
			return bytes.NewBufferString("{}"), nil
		},
		httpPost: func(io.Reader) (*http.Response, error) {
			return &http.Response{
				Status:     "404 NOT FOUND",
				StatusCode: 400,
				Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
			}, nil
		},
	}

	resp, err := rc.Fetch(&envoyapi.DiscoveryRequest{})
	assert.Nil(t, resp)
	assert.NonNil(t, err)
}

func TestUnmarshallerErrorReturnsError(t *testing.T) {
	rc := &restDiscoveryService{
		marshaller: func(*envoyapi.DiscoveryRequest) (io.Reader, error) {
			return bytes.NewBufferString("{}"), nil
		},
		httpPost: func(io.Reader) (*http.Response, error) {
			return &http.Response{
				Status:     "200 OK",
				StatusCode: 400,
				Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
			}, nil
		},
		unmarshaller: func(io.Reader) (*envoyapi.DiscoveryResponse, error) {
			return nil, errors.New("boom")
		},
	}

	resp, err := rc.Fetch(&envoyapi.DiscoveryRequest{})
	assert.Nil(t, resp)
	assert.NonNil(t, err)
}

func TestUnmarshallerReturnsDesiredResult(t *testing.T) {
	expectedRes := &envoyapi.DiscoveryResponse{}

	rc := &restDiscoveryService{
		marshaller: func(*envoyapi.DiscoveryRequest) (io.Reader, error) {
			return bytes.NewBufferString("{}"), nil
		},
		httpPost: func(io.Reader) (*http.Response, error) {
			return &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
				Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
			}, nil
		},
		unmarshaller: func(io.Reader) (*envoyapi.DiscoveryResponse, error) {
			return expectedRes, nil
		},
	}

	resp, err := rc.Fetch(&envoyapi.DiscoveryRequest{})
	assert.Equal(t, expectedRes, resp)
	assert.Nil(t, err)
}
