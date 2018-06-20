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
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	envoylog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	"github.com/turbinelabs/cache"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbntime "github.com/turbinelabs/nonstdlib/time"
)

// alsReporterConfig manages XDS logging of GRPC streamed data.
type alsReporterConfig struct {
	monitorAccessLog   bool
	monitorUpstreamLog bool

	countSuccesses    bool
	countRedirects    bool
	countClientErrors bool
	countServerErrors bool

	cacheSize      int
	reportInterval time.Duration
}

func (c alsReporterConfig) Make(source tbntime.Source) (*alsReporter, error) {
	valid := c.countSuccesses || c.countRedirects || c.countClientErrors || c.countServerErrors
	valid = valid && (c.monitorAccessLog || c.monitorUpstreamLog)
	valid = valid && c.cacheSize > 1 && c.reportInterval > time.Second
	if !valid {
		c.monitorAccessLog = false
		c.monitorUpstreamLog = false

		return &alsReporter{
			config: c,
			cache:  cache.NewNoopCountingCache(),
		}, nil
	}

	cache, err := cache.NewCountingCache(c.cacheSize)
	if err != nil {
		return nil, err
	}

	timer := source.NewTimer(c.reportInterval)

	r := &alsReporter{
		config: c,
		cache:  cache,
		timer:  timer,
	}

	go r.reporterLoop(console.Info())

	return r, nil
}

type alsReporter struct {
	config alsReporterConfig
	cache  cache.CountingCache
	timer  tbntime.Timer
}

type report struct {
	count int
	key   string
}

type reports []report

func (r reports) Len() int           { return len(r) }
func (r reports) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r reports) Less(i, j int) bool { return r[i].count > r[j].count }

func (r *alsReporter) reporterLoop(logger *log.Logger) {
	for {
		<-r.timer.C()
		r.timer.Reset(r.config.reportInterval)

		entries := make(reports, 0, r.cache.Len())
		r.cache.ForEach(func(key string, n int) {
			entries = append(entries, report{n, key})
		})

		if len(entries) == 0 {
			logger.Println("ALS: no logged requests during reporting interval")
			continue
		}

		sort.Sort(entries)
		width := int(math.Log10(float64(entries[0].count))) + 1

		for _, entry := range entries {
			r.cache.Add(entry.key, -entry.count)
			logger.Printf("ALS: %[1]*d: %s", width, entry.count, entry.key)
		}
	}
}

func (r *alsReporter) count(
	logSource string,
	req *envoylog.HTTPRequestProperties,
	resp *envoylog.HTTPResponseProperties,
) {
	respCode := uint32(0)
	if resp.ResponseCode != nil {
		respCode = resp.ResponseCode.Value
	}

	switch respCode / 100 {
	case 1, 2:
		if !r.config.countSuccesses {
			return
		}
	case 3:
		if !r.config.countRedirects {
			return
		}
	case 4:
		if !r.config.countClientErrors {
			return
		}
	default:
		if !r.config.countServerErrors {
			return
		}
	}

	var port string
	if req.Port != nil {
		port = fmt.Sprintf(":%d", req.Port.Value)
	}

	key := fmt.Sprintf(
		"%3d %s://%s%s%s",
		respCode,
		req.Scheme,
		req.Authority,
		port,
		strings.Split(req.Path, "?")[0],
	)

	if r.config.monitorAccessLog && r.config.monitorUpstreamLog {
		key = fmt.Sprintf("%s (%s)", key, logSource)
	}

	r.cache.Inc(key)
}

func (r *alsReporter) reportAccessLog(
	req *envoylog.HTTPRequestProperties,
	resp *envoylog.HTTPResponseProperties,
) {
	if r.config.monitorAccessLog {
		r.count("access", req, resp)
	}
}

func (r *alsReporter) reportUpstreamLog(
	req *envoylog.HTTPRequestProperties,
	resp *envoylog.HTTPResponseProperties,
) {
	if r.config.monitorUpstreamLog {
		r.count("upstream", req, resp)
	}
}
