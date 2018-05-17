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
	"io"
	"testing"

	"github.com/golang/mock/gomock"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

func TestNewXDSFromFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStatsFromFlags := stats.NewMockFromFlags(ctrl)
	flagset := tbnflag.NewTestFlagSet()

	ff := NewXDSFromFlags(flagset, mockStatsFromFlags).(*xdsFromFlags)
	assert.Equal(t, ff.addr.Addr(), defaultAddr)
	flagset.Parse([]string{"--addr=:60000"})
	assert.Equal(t, ff.addr.Addr(), ":60000")
	assert.Equal(t, ff.caFile, "")
	flagset.Parse([]string{"--ca-file=/etc/tls/ca.pem"})
	assert.Equal(t, ff.caFile, "/etc/tls/ca.pem")
}

func TestXDSFromFlagsMake(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockStatsFromFlags := stats.NewMockFromFlags(ctrl)
	mockStats := stats.NewMockStats(ctrl)
	mockStatsFromFlags.EXPECT().Make().Return(mockStats, nil)

	mockRegistrar := poller.NewMockRegistrar(ctrl)

	ff := xdsFromFlags{
		addr:           tbnflag.NewHostPort(defaultAddr),
		statsFromFlags: mockStatsFromFlags,
		caFile:         "/etc/tls/ca.pem",
	}

	x, err := ff.Make(mockRegistrar)
	assert.Nil(t, err)
	xImpl := x.(*xds)
	assert.NonNil(t, xImpl.Consumer)
	assert.Equal(t, xImpl.addr, defaultAddr)
	assert.NonNil(t, xImpl.server)
	assert.NonNil(t, xImpl.logServer)
	assert.ArrayEqual(t, xImpl.closers.closers, []io.Closer{mockStats})
}
