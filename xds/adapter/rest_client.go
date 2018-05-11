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
	"fmt"
	"io"
	"net/http"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/jsonpb"
)

const (
	cdsPath                 = "/v2/discovery:clusters"
	cdsRestEndpointTemplate = "http://%s" + cdsPath
	edsRestEndpointTemplate = "http://%s/v2/discovery:endpoints"
)

// newRESTClusterService returns a clusterService backed by a REST CDS server at
// the specified host/port address
func newRESTClusterService(hostPortStr string) clusterService {
	return clusterService(
		newRESTDiscoveryService(fmt.Sprintf(cdsRestEndpointTemplate, hostPortStr)),
	)
}

// newRESTEndpointService returns an endpointService backed by a REST EDS server
// at the specified host/port address
func newRESTEndpointService(hostPortStr string) endpointService {
	return endpointService(
		newRESTDiscoveryService(fmt.Sprintf(edsRestEndpointTemplate, hostPortStr)),
	)
}

func newRESTDiscoveryService(endpoint string) discoveryService {
	pm := &jsonpb.Marshaler{}

	return &restDiscoveryService{
		marshaller: func(dr *envoyapi.DiscoveryRequest) (io.Reader, error) {
			b := &bytes.Buffer{}
			if err := pm.Marshal(b, dr); err != nil {
				return nil, err
			}

			return b, nil
		},
		unmarshaller: func(r io.Reader) (*envoyapi.DiscoveryResponse, error) {
			dr := &envoyapi.DiscoveryResponse{}
			if err := jsonpb.Unmarshal(r, dr); err != nil {
				return nil, err
			}

			return dr, nil

		},
		httpPost: func(r io.Reader) (*http.Response, error) {
			return http.Post(endpoint, "application/json", r)
		},
	}
}

type restDiscoveryService struct {
	marshaller   func(*envoyapi.DiscoveryRequest) (io.Reader, error)
	unmarshaller func(io.Reader) (*envoyapi.DiscoveryResponse, error)
	httpPost     func(io.Reader) (*http.Response, error)
}

func (rc *restDiscoveryService) Close() error {
	return nil
}

func (rc *restDiscoveryService) Fetch(
	req *envoyapi.DiscoveryRequest,
) (*envoyapi.DiscoveryResponse, error) {
	reader, err := rc.marshaller(req)
	if err != nil {
		return nil, err
	}

	resp, err := rc.httpPost(reader)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Non-200 XDS[%s] status code: %d",
			cdsPath,
			resp.StatusCode,
		)
	}

	return rc.unmarshaller(resp.Body)
}
