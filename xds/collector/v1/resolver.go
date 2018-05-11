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
	"errors"
	"fmt"
	"net/http"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/rotor/xds/collector"
)

const (
	sdsBasePath    = "/v1/registration"
	sdsURLTemplate = "http://%s:%d" + sdsBasePath
)

type sdsResp struct {
	Hosts []sdsHost `json:"hosts"`
}

type sdsHost struct {
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`
}

// NewClusterResolver returns a ClusterResolver backed by an SDS server at the
// host/port specified
func NewClusterResolver(
	host string,
	port int,
	client func(string) (*http.Response, error),
) collector.ClusterResolver {
	codec := codec.NewJson()
	baseURL := fmt.Sprintf(sdsURLTemplate, host, port)

	return func(cName string) (api.Instances, []error) {
		errs := []error{}
		if cName == "" {
			errs = append(
				errs,
				errors.New("Empty service name provided for ClusterResolver"),
			)

			return nil, errs
		}

		fullURL := baseURL + "/" + cName
		httpResp, err := client(fullURL)
		if err != nil {
			errs = append(errs, err)
			return nil, errs
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			errs = append(errs, fmt.Errorf(
				"Non-200 SDS[%s] status code: %d",
				sdsBasePath+"/"+cName,
				httpResp.StatusCode,
			))
			return nil, errs
		}

		var resp sdsResp
		if err = codec.Decode(httpResp.Body, &resp); err != nil {
			return nil, append(errs, err)
		}

		instances := make(api.Instances, len(resp.Hosts))
		for idx, h := range resp.Hosts {
			i := api.Instance{
				Host: h.IPAddress,
				Port: h.Port,
			}

			instances[idx] = i
		}

		return instances, errs
	}
}
