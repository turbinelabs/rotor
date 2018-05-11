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
	"testing"
	"time"

	envoylog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	"github.com/gogo/protobuf/types"

	"github.com/turbinelabs/cache"
	tbntime "github.com/turbinelabs/nonstdlib/time"
	"github.com/turbinelabs/test/assert"
	testlog "github.com/turbinelabs/test/log"
)

func TestALSReporterConfig(t *testing.T) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		arc := alsReporterConfig{}
		ar, err := arc.Make(cs)
		assert.Nil(t, err)
		assert.DeepEqual(t, ar.config, arc)
		assert.NonNil(t, ar.cache)
		ar.cache.Add("x", 1)
		assert.Equal(t, ar.cache.Get("x"), 0)
		assert.Nil(t, ar.timer)

		arc.monitorAccessLog = true
		arc.cacheSize = 100
		arc.reportInterval = time.Minute
		ar, err = arc.Make(cs)
		assert.Nil(t, err)
		assert.NotDeepEqual(t, ar.config, arc)
		assert.NonNil(t, ar.cache)
		ar.cache.Add("x", 1)
		assert.Equal(t, ar.cache.Get("x"), 0)
		assert.Nil(t, ar.timer)

		arc.monitorAccessLog = true
		arc.countServerErrors = true
		arc.cacheSize = 100
		arc.reportInterval = time.Minute
		ar, err = arc.Make(cs)
		assert.Nil(t, err)
		assert.DeepEqual(t, ar.config, arc)
		assert.NonNil(t, ar.cache)
		ar.cache.Add("x", 1)
		assert.Equal(t, ar.cache.Get("x"), 1)
		assert.NonNil(t, ar.timer)
	})
}

func TestALSReporterAccessLog(t *testing.T) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		arc := alsReporterConfig{
			monitorAccessLog:  true,
			countServerErrors: true,
			cacheSize:         10,
			reportInterval:    time.Minute,
		}
		ar, err := arc.Make(cs)
		assert.Nil(t, err)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-upstream",
			},
			&envoylog.HTTPResponseProperties{},
		)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-success",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{200},
			},
		)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-redirect",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{300},
			},
		)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-client-error",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{400},
			},
		)

		assert.Equal(t, ar.cache.Len(), 0)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/logged-500",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{500},
			},
		)

		assert.Equal(t, ar.cache.Len(), 1)
		assert.Equal(t, ar.cache.Get("500 http://example.com/logged-500"), 1)
		ar.cache.Clear()

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "https",
				Authority: "example.com",
				Port:      &types.UInt32Value{8080},
				Path:      "/logged-500",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{500},
			},
		)

		assert.Equal(t, ar.cache.Len(), 1)
		assert.Equal(t, ar.cache.Get("500 https://example.com:8080/logged-500"), 1)
	})
}

func TestALSReporterUpstreamLog(t *testing.T) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		arc := alsReporterConfig{
			monitorUpstreamLog: true,
			countClientErrors:  true,
			cacheSize:          10,
			reportInterval:     time.Minute,
		}
		ar, err := arc.Make(cs)
		assert.Nil(t, err)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-access",
			},
			&envoylog.HTTPResponseProperties{},
		)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-success",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{200},
			},
		)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-redirect",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{300},
			},
		)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/ignored-server-error",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{500},
			},
		)

		assert.Equal(t, ar.cache.Len(), 0)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/client-error",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{400},
			},
		)

		assert.Equal(t, ar.cache.Len(), 1)
		assert.Equal(t, ar.cache.Get("400 http://example.com/client-error"), 1)
	})
}

func TestALSReporterAccessAndUpstreamLog(t *testing.T) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		arc := alsReporterConfig{
			monitorAccessLog:   true,
			monitorUpstreamLog: true,
			countSuccesses:     true,
			cacheSize:          10,
			reportInterval:     time.Minute,
		}
		ar, err := arc.Make(cs)
		assert.Nil(t, err)

		ar.reportAccessLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/access",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{200},
			},
		)

		ar.reportUpstreamLog(
			&envoylog.HTTPRequestProperties{
				Scheme:    "http",
				Authority: "example.com",
				Path:      "/upstream",
			},
			&envoylog.HTTPResponseProperties{
				ResponseCode: &types.UInt32Value{201},
			},
		)

		assert.Equal(t, ar.cache.Len(), 2)
		assert.Equal(t, ar.cache.Get("200 http://example.com/access (access)"), 1)
		assert.Equal(t, ar.cache.Get("201 http://example.com/upstream (upstream)"), 1)
	})
}

func TestALSReporterLoop(t *testing.T) {
	tbntime.WithCurrentTimeFrozen(func(cs tbntime.ControlledSource) {
		cache, err := cache.NewCountingCache(10)
		assert.Nil(t, err)

		ar := alsReporter{
			config: alsReporterConfig{
				monitorAccessLog:  true,
				countServerErrors: true,
				cacheSize:         10,
				reportInterval:    time.Minute,
			},
			timer: cs.NewTimer(time.Minute),
			cache: cache,
		}

		log, ch := testlog.NewChannelLogger(10)
		go ar.reporterLoop(log)

		for i := 0; i < 10; i++ {
			for j := i; j < 10; j++ {
				r := uint32(510 - j)
				ar.reportAccessLog(
					&envoylog.HTTPRequestProperties{
						Scheme:    "http",
						Authority: "example.com",
						Path:      fmt.Sprintf("/logged-%d", r),
					},
					&envoylog.HTTPResponseProperties{
						ResponseCode: &types.UInt32Value{r},
					},
				)
			}
		}

		cs.Advance(time.Minute)

		assert.Equal(t, <-ch, "ALS: 10: 501 http://example.com/logged-501\n")
		assert.Equal(t, <-ch, "ALS:  9: 502 http://example.com/logged-502\n")
		assert.Equal(t, <-ch, "ALS:  8: 503 http://example.com/logged-503\n")
		assert.Equal(t, <-ch, "ALS:  7: 504 http://example.com/logged-504\n")
		assert.Equal(t, <-ch, "ALS:  6: 505 http://example.com/logged-505\n")
		assert.Equal(t, <-ch, "ALS:  5: 506 http://example.com/logged-506\n")
		assert.Equal(t, <-ch, "ALS:  4: 507 http://example.com/logged-507\n")
		assert.Equal(t, <-ch, "ALS:  3: 508 http://example.com/logged-508\n")
		assert.Equal(t, <-ch, "ALS:  2: 509 http://example.com/logged-509\n")
		assert.Equal(t, <-ch, "ALS:  1: 510 http://example.com/logged-510\n")
		assert.ChannelEmpty(t, ch)

		cs.Advance(time.Minute)
		assert.Equal(t, <-ch, "ALS: no logged requests during reporting interval\n")
	})
}
