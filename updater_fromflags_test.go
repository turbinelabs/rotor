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

package rotor

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	apiflags "github.com/turbinelabs/api/client/flags"
	"github.com/turbinelabs/api/service"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/rotor/xds/adapter"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/stats"
	"github.com/turbinelabs/test/assert"
)

type uffMocks struct {
	ff                 updaterFromFlags
	apiConfigFromFlags *apiflags.MockAPIConfigFromFlags
	apiClientFromFlags *apiflags.MockClientFromFlags
	zoneFromFlags      *apiflags.MockZoneFromFlags
	updaterFromFlags   *updater.MockFromFlags
	xdsFromFlags       *adapter.MockXDSFromFlags
	pollerFromFlags    *poller.MockFromFlags
	statsFromFlags     *stats.MockFromFlags
	ctrl               *gomock.Controller
	calls              []*gomock.Call
}

func (m *uffMocks) expect(calls ...*gomock.Call) {
	calls = append(m.calls, calls...)
}

func (m *uffMocks) finish(f func()) {
	gomock.InOrder(m.calls...)
	f()
	m.ctrl.Finish()
}

func newUFFMocks(t *testing.T) *uffMocks {
	ctrl := gomock.NewController(assert.Tracing(t))
	uffm := &uffMocks{
		apiConfigFromFlags: apiflags.NewMockAPIConfigFromFlags(ctrl),
		apiClientFromFlags: apiflags.NewMockClientFromFlags(ctrl),
		zoneFromFlags:      apiflags.NewMockZoneFromFlags(ctrl),
		updaterFromFlags:   updater.NewMockFromFlags(ctrl),
		xdsFromFlags:       adapter.NewMockXDSFromFlags(ctrl),
		pollerFromFlags:    poller.NewMockFromFlags(ctrl),
		statsFromFlags:     stats.NewMockFromFlags(ctrl),
		ctrl:               ctrl,
	}
	uffm.ff = updaterFromFlags{
		apiConfigFromFlags: uffm.apiConfigFromFlags,
		apiClientFromFlags: uffm.apiClientFromFlags,
		zoneFromFlags:      uffm.zoneFromFlags,
		updaterFromFlags:   uffm.updaterFromFlags,
		xdsFromFlags:       uffm.xdsFromFlags,
		pollerFromFlags:    uffm.pollerFromFlags,
		statsFromFlags:     uffm.statsFromFlags,
		standalonePort:     1234,
	}
	return uffm
}

type uffValidateTestCase struct {
	validateXDSOnly          bool
	disableXDS               bool
	apiKey                   string
	zoneName                 string
	updaterValidateErr       error
	pollerValidateErr        error
	apiClientValidateErr     error
	wantErr                  error
	exitBeforeZoneName       bool
	exitBeforeClientValidate bool
}

func (tc uffValidateTestCase) run(t *testing.T) {
	mocks := newUFFMocks(t)
	mocks.ff.disableXDS = tc.disableXDS

	defer mocks.finish(func() {
		var gotErr error
		if tc.validateXDSOnly {
			gotErr = mocks.ff.ValidateXDSOnly()
		} else {
			gotErr = mocks.ff.Validate()
		}
		assert.DeepEqual(t, gotErr, tc.wantErr)
	})

	if tc.updaterValidateErr != nil {
		mocks.expect(mocks.updaterFromFlags.EXPECT().Validate().Return(tc.updaterValidateErr))
		return
	}

	mocks.expect(mocks.updaterFromFlags.EXPECT().Validate().Return(nil))

	if tc.pollerValidateErr != nil {
		mocks.expect(mocks.pollerFromFlags.EXPECT().Validate().Return(tc.pollerValidateErr))
		return
	}

	mocks.expect(
		mocks.pollerFromFlags.EXPECT().Validate().Return(nil),
		mocks.apiConfigFromFlags.EXPECT().APIKey().Return(tc.apiKey),
	)

	if tc.exitBeforeZoneName {
		return
	}

	mocks.expect(mocks.zoneFromFlags.EXPECT().Name().Return(tc.zoneName))

	if tc.exitBeforeClientValidate {
		return
	}

	if tc.apiClientValidateErr != nil {
		mocks.expect(mocks.apiClientFromFlags.EXPECT().Validate().Return(tc.apiClientValidateErr))
		return
	}

	mocks.expect(mocks.apiClientFromFlags.EXPECT().Validate().Return(nil))
}

func TestUpdaterFromFlagsValidateUpdaterErr(t *testing.T) {
	err := errors.New("boom")
	uffValidateTestCase{
		updaterValidateErr: err,
		wantErr:            err,
	}.run(t)
}

func TestUpdaterFromFlagsValidatePollerErr(t *testing.T) {
	err := errors.New("boom")
	uffValidateTestCase{
		pollerValidateErr: err,
		wantErr:           err,
	}.run(t)
}

func TestUpdaterFromFlagsValidateApiKeyNoZoneName(t *testing.T) {
	err := errors.New("--api.zone-name must be specified if --api.key is specified")
	uffValidateTestCase{
		apiKey: "apikey",
		exitBeforeClientValidate: true,
		wantErr:                  err,
	}.run(t)
}

func TestUpdaterFromFlagsValidateClientErr(t *testing.T) {
	err := errors.New("--api.zone-name must be specified if --api.key is specified")
	uffValidateTestCase{
		apiClientValidateErr: err,
		apiKey:               "apikey",
		zoneName:             "zonename",
		wantErr:              err,
	}.run(t)
}

func TestUpdaterFromFlagsValidateXDSOnlyDisabledError(t *testing.T) {
	err := errors.New("xDS is disabled, but xds-only was requested")
	uffValidateTestCase{
		validateXDSOnly:    true,
		disableXDS:         true,
		exitBeforeZoneName: true,
		wantErr:            err,
	}.run(t)
}

func TestUpdaterFromFlagsValidateXDSOnlyMissingAPIKey(t *testing.T) {
	err := errors.New("--api.key must be specified for xds-only")
	uffValidateTestCase{
		validateXDSOnly:    true,
		exitBeforeZoneName: true,
		wantErr:            err,
	}.run(t)
}

func TestUpdaterFromFlagsValidateOKNoApiKey(t *testing.T) {
	uffValidateTestCase{
		exitBeforeZoneName: true,
	}.run(t)
}

func TestUpdaterFromFlagsValidateOK(t *testing.T) {
	uffValidateTestCase{
		apiKey:   "apikey",
		zoneName: "zonename",
	}.run(t)
}

type uffMakeTestCase struct {
	apiKey           string
	zoneName         string
	disableXDS       bool
	apiClientMakeErr error
	zoneRefGetErr    error
	updaterMakeErr   error
	statsMakeErr     error
	xdsMakeErr       error
	wantStandalone   bool
	wantErr          error
}

func (tc uffMakeTestCase) run(t *testing.T) {
	mocks := newUFFMocks(t)
	mocks.ff.disableXDS = tc.disableXDS
	mocks.ff.startXDS = mocks.ff.syncStartXDS
	mocks.ff.pollLoop = syncPollLoop

	mockXDS := adapter.NewMockXDS(mocks.ctrl)
	mockUpdater := updater.NewMockUpdater(mocks.ctrl)
	mockRegistrar := poller.NewMockRegistrar(mocks.ctrl)

	expectNewUpdaterCalled := tc.wantStandalone
	newUpdaterCalled := false
	defer func() {
		if expectNewUpdaterCalled {
			assert.True(t, newUpdaterCalled)
		}
	}()

	defer mocks.finish(func() {
		if tc.wantErr == nil && !tc.disableXDS {
			mocks.expect(mockXDS.EXPECT().Run())
		}

		got, gotErr := mocks.ff.Make()
		assert.DeepEqual(t, gotErr, tc.wantErr)
		if tc.wantErr != nil {
			assert.Nil(t, got)
			return
		}

		if tc.disableXDS {
			assert.Equal(t, got, mockUpdater)
		} else {
			ux, ok := got.(*updaterWithXDS)
			assert.True(t, ok)
			assert.Equal(t, ux.Updater, mockUpdater)
			assert.Equal(t, ux.xds, mockXDS)
		}
	})

	mocks.expect(mocks.apiConfigFromFlags.EXPECT().APIKey().Return(tc.apiKey))

	if tc.wantStandalone {
		if tc.disableXDS {
			expectNewUpdaterCalled = false
			return
		}

		newUpdater := func(c poller.Consumer) updater.Updater {
			newUpdaterCalled = true
			assert.DeepEqual(t, c, mockXDS)
			return mockUpdater
		}
		mocks.expect(
			mocks.updaterFromFlags.EXPECT().MakeStandalone(1234).Return(newUpdater, mockRegistrar),
		)

		if tc.xdsMakeErr != nil {
			expectNewUpdaterCalled = false
			mocks.expect(
				mocks.xdsFromFlags.EXPECT().Make(mockRegistrar).Return(nil, tc.xdsMakeErr),
			)
			return
		}

		mocks.expect(
			mocks.xdsFromFlags.EXPECT().Make(mockRegistrar).Return(mockXDS, nil),
		)
		return
	}

	if tc.apiClientMakeErr != nil {
		mocks.expect(mocks.apiClientFromFlags.EXPECT().Make().Return(nil, tc.apiClientMakeErr))
		return
	}

	svc := service.NewMockAll(mocks.ctrl)
	mocks.expect(mocks.apiClientFromFlags.EXPECT().Make().Return(svc, nil))

	if !tc.disableXDS {
		if tc.statsMakeErr != nil {
			mocks.expect(mocks.statsFromFlags.EXPECT().Make().Return(nil, tc.statsMakeErr))
			return
		}

		sc := stats.NewMockStats(mocks.ctrl)
		ca := poller.NewMockPoller(mocks.ctrl)

		mocks.expect(
			mocks.statsFromFlags.EXPECT().Make().Return(sc, nil),
			sc.EXPECT().AddTags(stats.NewKVTag(stats.ProxyVersionTag, constants.TbnPublicVersion)),
		)

		if tc.xdsMakeErr != nil {
			mocks.expect(
				mocks.xdsFromFlags.EXPECT().Make(gomock.Any()).Return(nil, tc.xdsMakeErr),
			)
			return
		}

		mocks.expect(
			mocks.xdsFromFlags.EXPECT().Make(gomock.Any()).Return(mockXDS, nil),
			mocks.pollerFromFlags.EXPECT().Make(svc, mockXDS, gomock.Any(), sc).Return(ca),
			ca.EXPECT().PollLoop(),
		)
	}

	zoneRef := service.NewMockZoneRef(mocks.ctrl)
	mocks.expect(mocks.zoneFromFlags.EXPECT().Ref().Return(zoneRef))
	if tc.zoneRefGetErr != nil {
		mocks.expect(zoneRef.EXPECT().Get(svc).Return(api.Zone{}, tc.zoneRefGetErr))
		return
	}

	z := api.Zone{ZoneKey: "zk"}
	mocks.expect(
		zoneRef.EXPECT().Get(svc).Return(z, nil),
		mocks.updaterFromFlags.EXPECT().Make(svc, z).Return(mockUpdater),
	)
}

func (tc uffMakeTestCase) runMakeXDS(t *testing.T) {
	mocks := newUFFMocks(t)
	mocks.ff.disableXDS = tc.disableXDS
	mocks.ff.startXDS = mocks.ff.syncStartXDS
	mocks.ff.pollLoop = syncPollLoop

	mockXDS := adapter.NewMockXDS(mocks.ctrl)

	defer mocks.finish(func() {
		got, gotErr := mocks.ff.MakeXDS()
		assert.DeepEqual(t, gotErr, tc.wantErr)
		if tc.wantErr == nil {
			assert.Equal(t, got, mockXDS)
		} else {
			assert.Nil(t, got)
		}
	})

	if tc.disableXDS {
		return
	}

	if tc.apiClientMakeErr != nil {
		mocks.expect(mocks.apiClientFromFlags.EXPECT().Make().Return(nil, tc.apiClientMakeErr))
		return
	}

	svc := service.NewMockAll(mocks.ctrl)
	mocks.expect(mocks.apiClientFromFlags.EXPECT().Make().Return(svc, nil))

	if tc.statsMakeErr != nil {
		mocks.expect(mocks.statsFromFlags.EXPECT().Make().Return(nil, tc.statsMakeErr))
		return
	}

	sc := stats.NewMockStats(mocks.ctrl)
	ca := poller.NewMockPoller(mocks.ctrl)

	mocks.expect(
		mocks.statsFromFlags.EXPECT().Make().Return(sc, nil),
		sc.EXPECT().AddTags(stats.NewKVTag(stats.ProxyVersionTag, constants.TbnPublicVersion)),
		mocks.xdsFromFlags.EXPECT().Make(gomock.Any()).Return(mockXDS, nil),
		mocks.pollerFromFlags.EXPECT().Make(svc, mockXDS, gomock.Any(), sc).Return(ca),
		ca.EXPECT().PollLoop(),
	)
}

func TestUpdaterFromFlagsMakeStandalone(t *testing.T) {
	uffMakeTestCase{
		wantStandalone: true,
	}.run(t)
}

func TestUpdaterFromFlagsMakeStandaloneXDSFromFlagsErr(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		xdsMakeErr:     err,
		wantStandalone: true,
		wantErr:        err,
	}.run(t)
}

func TestUpdaterFromFlagsMakeStandaloneWithXDSDisabled(t *testing.T) {
	err := errors.New("No --api.key specified. Cannot use standalone mode because xDS is disabled.")
	uffMakeTestCase{
		disableXDS:     true,
		wantStandalone: true,
		wantErr:        err,
	}.run(t)
}

func TestUpdaterFromFlagsMakeClientErr(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		apiKey:           "apikey",
		apiClientMakeErr: err,
		wantErr:          err,
	}.run(t)
}

func TestUpdaterFromFlagsMakeZoneErr(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		apiKey:        "apikey",
		zoneRefGetErr: err,
		wantErr:       err,
	}.run(t)
}

func TestUpdaterFromFlagsMakeXDSFromFlagsErr(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		apiKey:     "apikey",
		xdsMakeErr: err,
		wantErr:    err,
	}.run(t)
}

func TestUpdaterFromFlagsMake(t *testing.T) {
	uffMakeTestCase{
		apiKey: "apikey",
	}.run(t)
}

func TestUpdaterFromFlagsMakeDisabledXDS(t *testing.T) {
	uffMakeTestCase{
		disableXDS: true,
		apiKey:     "apikey",
	}.run(t)
}

func TestUpdaterFromFlagsMakeXDSStatsErr(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		apiKey:       "apikey",
		statsMakeErr: err,
		wantErr:      err,
	}.run(t)
}

func TestUpdaterFromFlagsMakeXDS(t *testing.T) {
	uffMakeTestCase{
		apiKey: "apikey",
	}.runMakeXDS(t)
}

func TestUpdaterFromFlagsMakeXDSError(t *testing.T) {
	err := errors.New("boom")
	uffMakeTestCase{
		apiKey:           "apikey",
		apiClientMakeErr: err,
		wantErr:          err,
	}.runMakeXDS(t)
}

func TestUpdaterFromFlagsMakeXDSWithDisabledXDS(t *testing.T) {
	err := errors.New("xDS is disabled")
	uffMakeTestCase{
		disableXDS: true,
		apiKey:     "apikey",
		wantErr:    err,
	}.runMakeXDS(t)
}
