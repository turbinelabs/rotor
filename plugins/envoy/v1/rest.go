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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/turbinelabs/api"
	tbnhttp "github.com/turbinelabs/api/http"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbnstrings "github.com/turbinelabs/nonstdlib/strings"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/collector/v1"
)

const (
	cdsPathRoot            = "/v1/clusters"
	cdsClusterTemplate     = cdsPathRoot + "/%s"
	cdsClusterNodeTemplate = cdsClusterTemplate + "/%s"
)

type restRunner struct {
	updaterFlags  rotor.UpdaterFromFlags
	addr          tbnflag.HostPort
	sdsAddr       tbnflag.HostPort
	clustersNodes tbnflag.Strings
}

func (r *restRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := r.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	updater, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	cdsEndp, cdsErr := tbnhttp.NewEndpoint(tbnhttp.HTTP, r.addr.Addr())
	if cdsErr != nil {
		return cmd.BadInput(cdsErr)
	}

	sdsEndp, sdsErr := tbnhttp.NewEndpoint(tbnhttp.HTTP, r.sdsAddr.Addr())
	if sdsErr != nil {
		return cmd.BadInput(sdsErr)
	}

	cdsRoutes, rErr := mkCdsRoutes(r.clustersNodes.Strings)
	if rErr != nil {
		return cmd.BadInput(rErr)
	}

	if len(cdsRoutes) == 0 {
		console.Info().Printf("No clustersNodes provided. Using CDS route \"/v1/clusters\"")
		cdsRoutes = append(cdsRoutes, cdsPathRoot)
	}

	host, port := r.sdsAddr.ParsedHostPort()
	parser := newCdsParser(v1.NewClusterResolver(host, port, sdsEndp.Client().Get)).parse
	clientFn := func(path string) (*http.Response, error) {
		fullURL := cdsEndp.URL(path, nil)
		return cdsEndp.Client().Get(fullURL)
	}

	collector := &restCollector{
		updater:   updater,
		cdsRoutes: cdsRoutes,
		clientFn:  clientFn,
		parserFn:  parser,
	}

	collector.updateLoop()

	return command.NoError()
}

func mkCdsRoutes(pairs []string) ([]string, error) {
	routes := make([]string, len(pairs))
	for idx, cn := range pairs {
		c, n := tbnstrings.SplitFirstColon(cn)
		switch {
		case strings.Contains(n, ":") || (c == "" && n == "") || c == "":
			return nil, fmt.Errorf(
				"clustersNodes must be of the form \"<cluster>:<node>\" or "+
					"\"<cluster>\", but was %s",
				cn,
			)

		case n == "":
			routes[idx] = fmt.Sprintf(cdsClusterTemplate, url.PathEscape(c))

		default:
			routes[idx] = fmt.Sprintf(
				cdsClusterNodeTemplate,
				url.PathEscape(c),
				url.PathEscape(n),
			)
		}
	}

	return routes, nil
}

type restCollector struct {
	updater   updater.Updater
	cdsRoutes []string
	clientFn  func(string) (*http.Response, error)
	parserFn  func(io.Reader) ([]api.Cluster, error)
}

func (rc *restCollector) updateLoop() {
	for {
		for _, cdsRoute := range rc.cdsRoutes {
			tbnClusters, err := rc.getClusters(cdsRoute)
			if err == nil {
				rc.updater.Replace(tbnClusters)
			} else {
				console.Error().Printf("Error handling CDS update for: %s", cdsRoute)
			}
		}

		time.Sleep(rc.updater.Delay())
	}
}

func (rc *restCollector) getClusters(route string) ([]api.Cluster, error) {
	httpResp, err := rc.clientFn(route)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Non-200 CDS[%s] status code: %d",
			route,
			httpResp.StatusCode,
		)
	}

	defer httpResp.Body.Close()

	return rc.parserFn(httpResp.Body)
}
