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
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/rotor/xds/poller"
	"github.com/turbinelabs/test/assert"
)

func mkTestListener(name, host string, port uint32) *envoyapi.Listener {
	return &envoyapi.Listener{
		Name: name,
		Address: envoycore.Address{
			Address: &envoycore.Address_SocketAddress{
				SocketAddress: &envoycore.SocketAddress{
					Protocol: envoycore.TCP,
					Address:  host,
					PortSpecifier: &envoycore.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
	}
}

var testCacheSnapshot = cache.Snapshot{
	Endpoints: cache.Resources{
		Version: "endpoints-" + poller.FixtureHash,
		Items: map[string]cache.Resource{
			"endpoint-1": &envoyapi.ClusterLoadAssignment{
				ClusterName: "cluster-1",
			},
		},
	},
	Clusters: cache.Resources{
		Version: "clusters-" + poller.FixtureHash,
		Items: map[string]cache.Resource{
			"cluster-1": &envoyapi.Cluster{
				Name: "cluster-1",
			},
		},
	},
	Routes: cache.Resources{
		Version: "routes-" + poller.FixtureHash,
		Items: map[string]cache.Resource{
			"route-1": &envoyapi.RouteConfiguration{
				Name: "route-1",
			},
		},
	},
	Listeners: cache.Resources{
		Version: "listeners-" + poller.FixtureHash,
		Items: map[string]cache.Resource{
			"listener-1": mkTestListener("listener-1", "127.0.0.1", 80),
		},
	},
}

type newSnapshotAdapterTestCase struct {
	endpointAdaptErr  error
	staticResources   staticResources
	clusterAdaptErr   error
	routeAdaptErr     error
	listenerAdaptErr  error
	listenerInjectErr error
	want              cache.Snapshot
	wantErr           error
}

func (tc newSnapshotAdapterTestCase) run(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))

	objs := poller.MkFixtureObjects()

	mockEndpointAdapter := newMockResourceAdapter(ctrl)
	mockClusterAdapter := newMockClusterAdapter(ctrl)
	mockResourceAdapter := newMockClusterAdapter(ctrl)
	mockListenerAdapter := newMockListenerAdapter(ctrl)
	mockProvider := newMockStaticResourcesProvider(ctrl)

	adapt := newSnapshotAdapter(
		mockEndpointAdapter,
		mockClusterAdapter,
		mockResourceAdapter,
		mockListenerAdapter,
		mockProvider,
	)

	calls := []*gomock.Call{}

	tc.staticResources.version = "static"

	defer func() {
		gomock.InOrder(calls...)

		got, gotErr := adapt(objs)
		ctrl.Finish()

		if tc.wantErr == nil {
			if len(tc.staticResources.clusters) > 0 || tc.staticResources.clusterTemplate != nil {
				tc.want.Clusters.Version = tc.want.Clusters.Version + "static"
			}

			if len(tc.staticResources.listeners) > 0 {
				tc.want.Listeners.Version = tc.want.Listeners.Version + "static"
			}
		}

		assert.DeepEqual(t, got, tc.want)
		assert.DeepEqual(t, gotErr, tc.wantErr)

	}()

	expect := func(cs ...*gomock.Call) {
		calls = append(calls, cs...)
	}

	expect(mockProvider.EXPECT().StaticResources().Return(tc.staticResources))

	if tc.endpointAdaptErr != nil {
		expect(mockEndpointAdapter.EXPECT().adapt(objs).Return(cache.Resources{}, tc.endpointAdaptErr))
		return
	}

	expect(
		mockEndpointAdapter.EXPECT().adapt(objs).Return(tc.want.Endpoints, nil),
		mockClusterAdapter.EXPECT().withTemplate(tc.staticResources.clusterTemplate).Return(mockClusterAdapter),
	)

	if tc.clusterAdaptErr != nil {
		expect(mockClusterAdapter.EXPECT().adapt(objs).Return(cache.Resources{}, tc.clusterAdaptErr))
		return
	}

	expect(mockClusterAdapter.EXPECT().adapt(objs).Return(tc.want.Clusters, nil))

	if tc.routeAdaptErr != nil {
		expect(mockResourceAdapter.EXPECT().adapt(objs).Return(cache.Resources{}, tc.routeAdaptErr))
		return
	}

	expect(mockResourceAdapter.EXPECT().adapt(objs).Return(tc.want.Routes, nil))

	if tc.listenerAdaptErr != nil {
		expect(mockListenerAdapter.EXPECT().adapt(objs).Return(cache.Resources{}, tc.listenerAdaptErr))
		return
	}

	expect(mockListenerAdapter.EXPECT().adapt(objs).Return(tc.want.Listeners, nil))

	if len(tc.staticResources.listeners) != 0 {
		if tc.listenerInjectErr != nil {
			expect(mockListenerAdapter.EXPECT().inject(tc.staticResources.listeners[0]).Return(tc.listenerInjectErr))
			return
		}
		for i := range tc.staticResources.listeners {
			expect(mockListenerAdapter.EXPECT().inject(tc.staticResources.listeners[i]).Return(nil))
		}
	}
}

func TestNewSnapshotAdapterEndpointAdaptErr(t *testing.T) {
	err := errors.New("boom")
	newSnapshotAdapterTestCase{
		endpointAdaptErr: err,
		wantErr:          err,
	}.run(t)
}

func TestNewSnapshotAdapterClusterAdaptErr(t *testing.T) {
	err := errors.New("boom")
	newSnapshotAdapterTestCase{
		clusterAdaptErr: err,
		wantErr:         err,
	}.run(t)
}

func TestNewSnapshotAdapterRouteAdaptErr(t *testing.T) {
	err := errors.New("boom")
	newSnapshotAdapterTestCase{
		routeAdaptErr: err,
		wantErr:       err,
	}.run(t)
}

func TestNewSnapshotAdapterListenerAdaptErr(t *testing.T) {
	err := errors.New("boom")
	newSnapshotAdapterTestCase{
		listenerAdaptErr: err,
		wantErr:          err,
	}.run(t)
}

func TestNewSnapshotAdapterSuccess(t *testing.T) {
	newSnapshotAdapterTestCase{
		want: testCacheSnapshot,
	}.run(t)
}

func TestNewSnapshotAdapterClusterTemplate(t *testing.T) {
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			clusterTemplate: &envoyapi.Cluster{
				Name: "some deal",
			},
		},
		want: testCacheSnapshot,
	}.run(t)
}

func TestNewSnapshotAdapterStaticClustersMerge(t *testing.T) {
	want := testCacheSnapshot
	want.Clusters.Items = map[string]cache.Resource{
		"cluster-1": &envoyapi.Cluster{
			Name: "cluster-1",
		},
		"cluster-2": &envoyapi.Cluster{
			Name: "cluster-2",
		},
	}
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			clusters: []*envoyapi.Cluster{
				{
					Name: "cluster-2",
				},
			},
		},
		want: want,
	}.run(t)
}

func TestNewSnapshotAdapterStaticClustersOverwrite(t *testing.T) {
	want := testCacheSnapshot
	want.Clusters.Items = map[string]cache.Resource{
		"cluster-2": &envoyapi.Cluster{
			Name: "cluster-2",
		},
	}
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			clusters: []*envoyapi.Cluster{
				{
					Name: "cluster-2",
				},
			},
			conflictBehavior: overwriteBehavior,
		},
		want: want,
	}.run(t)
}

func TestNewSnapshotAdapterStaticListenersInjectErr(t *testing.T) {
	ch, cleanup := console.ConsumeConsoleLogs(10)
	defer cleanup()

	err := errors.New("boom")
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			listeners: []*envoyapi.Listener{
				mkTestListener("listener-2", "127.0.0.1", 81),
			},
		},
		listenerInjectErr: err,
		want:              testCacheSnapshot,
	}.run(t)

	msg := <-ch
	assert.Equal(
		t,
		msg.Message,
		"[error] failed to inject ALS logging into static listener listener-2: boom\n",
	)
}

func TestNewSnapshotAdapterStaticListenersMerge(t *testing.T) {
	want := testCacheSnapshot
	want.Listeners.Items = map[string]cache.Resource{
		"listener-1": mkTestListener("listener-1", "127.0.0.1", 80),
		"listener-2": mkTestListener("listener-2", "127.0.0.1", 81),
	}
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			listeners: []*envoyapi.Listener{
				mkTestListener("listener-2", "127.0.0.1", 81),
			},
		},
		want: want,
	}.run(t)
}

func TestNewSnapshotAdapterStaticListenersOverwrite(t *testing.T) {
	want := testCacheSnapshot
	want.Listeners.Items = map[string]cache.Resource{
		"listener-2": mkTestListener("listener-2", "127.0.0.1", 81),
	}
	newSnapshotAdapterTestCase{
		staticResources: staticResources{
			listeners: []*envoyapi.Listener{
				mkTestListener("listener-2", "127.0.0.1", 81),
			},
			conflictBehavior: overwriteBehavior,
		},
		want: want,
	}.run(t)
}

// Coverts the given interface{} into a *structpb.Value. The interface
// may be of type string, bool, float64, int, or int64. In addition it
// may be a map[string]interface (resulting a struct-type Value), or
// an array/slice of any of supported type (resulting in a list-type
// Value). Conversion errors result in the test being failed. Not
// suitable for production code due to reflection, and lack of
// complete type support.
func ifaceToValue(tb testing.TB, i interface{}) *types.Value {
	if i == nil {
		return &types.Value{
			Kind: &types.Value_NullValue{NullValue: types.NULL_VALUE},
		}
	}

	switch v := i.(type) {
	case bool:
		return &types.Value{Kind: &types.Value_BoolValue{BoolValue: v}}
	case float64:
		return &types.Value{Kind: &types.Value_NumberValue{NumberValue: v}}
	case int:
		return &types.Value{Kind: &types.Value_NumberValue{NumberValue: float64(v)}}
	case int64:
		return &types.Value{Kind: &types.Value_NumberValue{NumberValue: float64(v)}}
	case string:
		return &types.Value{Kind: &types.Value_StringValue{StringValue: v}}
	default:
		val := reflect.ValueOf(i)
		switch val.Kind() {
		case reflect.Array, reflect.Slice:
			values := make([]*types.Value, val.Len())
			for i := 0; i < val.Len(); i++ {
				values[i] = ifaceToValue(tb, val.Index(i).Interface())
				if values[i] == nil {
					return nil
				}
			}
			return &types.Value{Kind: &types.Value_ListValue{
				ListValue: &types.ListValue{
					Values: values,
				},
			}}
		case reflect.Map:
			t := val.Type()
			if t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface {
				return &types.Value{Kind: &types.Value_StructValue{
					StructValue: mapToStruct(tb, v.(map[string]interface{})),
				}}
			}
			fallthrough
		default:
			tb.Fatalf("cannot encode value %q of type %T", i, i)
			return nil
		}
	}
}

// Converts a simple go map to a protobuf Struct. The map values may
// be of type string, bool, float64, int, or int64. In addition a map
// value be a nested map[string]interface, or an array/slice of any of
// supported type. Conversion errors result in failing the test. Not
// suitable for production code due to reflection, and lack of
// complete type support.
func mapToStruct(tb testing.TB, m map[string]interface{}) *types.Struct {
	fields := make(map[string]*types.Value, len(m))

	for k, i := range m {
		v := ifaceToValue(tb, i)
		if v == nil {
			return nil
		}
		fields[k] = v
	}

	return &types.Struct{Fields: fields}
}

type mockT struct {
	testing.TB

	messages []string
}

func (t *mockT) Fatalf(format string, args ...interface{}) {
	t.messages = append(t.messages, fmt.Sprintf(format, args...))
}

func TestMapToStruct(t *testing.T) {
	m := map[string]interface{}{
		"bool":   true,
		"f64":    1.234,
		"i":      int(123),
		"i64":    int64(1234),
		"list":   []string{"a", "b", "c"},
		"string": "a string",
		"struct": map[string]interface{}{
			"nested": "x",
		},
	}

	s := &types.Struct{
		Fields: map[string]*types.Value{
			"bool": {Kind: &types.Value_BoolValue{BoolValue: true}},
			"f64":  {Kind: &types.Value_NumberValue{NumberValue: 1.234}},
			"i":    {Kind: &types.Value_NumberValue{NumberValue: 123.0}},
			"i64":  {Kind: &types.Value_NumberValue{NumberValue: 1234}},
			"list": {Kind: &types.Value_ListValue{ListValue: &types.ListValue{
				Values: []*types.Value{
					valueString("a"),
					valueString("b"),
					valueString("c"),
				},
			}}},
			"string": valueString("a string"),
			"struct": {Kind: &types.Value_StructValue{StructValue: &types.Struct{
				Fields: map[string]*types.Value{
					"nested": valueString("x"),
				},
			}}},
		},
	}

	mockT := &mockT{}

	assert.DeepEqual(t, mapToStruct(mockT, m), s)
	assert.Nil(t, mockT.messages)

	assert.Nil(t, mapToStruct(mockT, map[string]interface{}{"x": errors.New("?")}))
	assert.Equal(t, len(mockT.messages), 1)
	assert.HasPrefix(t, mockT.messages[0], "cannot encode value ")
}

func TestHTTPSRedirectFullySpecifiedHostWithPort(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://foo.example.com:8443$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.True(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectFullySpecifiedHostNoPort(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://foo.example.com$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.True(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectVariableHost(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://$host$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.True(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectVariableHostWithPort(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.True(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectVariableHostWithPortWithXFP(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
		HeaderConstraints: tbnapi.HeaderConstraints{{
			Name:   "X-Forwarded-Proto",
			Value:  "https",
			Invert: true,
		}},
	}

	assert.True(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadName(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadFrom(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.**)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadRedirectType(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.TemporaryRedirect,
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadXFPWrongHeader(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
		HeaderConstraints: tbnapi.HeaderConstraints{{
			Name:   "X-Forwarded-For",
			Value:  "https",
			Invert: true,
		}},
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadXFPWrongValue(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
		HeaderConstraints: tbnapi.HeaderConstraints{{
			Name:   "X-Forwarded-Proto",
			Value:  "http",
			Invert: true,
		}},
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectBadXFPNotInverted(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-http",
		From:         "(.*)",
		To:           "https://$host:443$1",
		RedirectType: tbnapi.PermanentRedirect,
		HeaderConstraints: tbnapi.HeaderConstraints{{
			Name:  "X-Forwarded-Proto",
			Value: "https",
		}},
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectMultipleCaptures(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://$host$1:443$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestHTTPSRedirectMultipleCapturesInPort(t *testing.T) {
	input := tbnapi.Redirect{
		Name:         "force-https",
		From:         "(.*)",
		To:           "https://$host:443$1$1",
		RedirectType: tbnapi.PermanentRedirect,
	}

	assert.False(t, isHTTPSRedirect("foo.example.com", input))
}

func TestBoolValue(t *testing.T) {
	t1 := boolValue(true)
	t2 := boolValue(true)
	f := boolValue(false)

	assert.True(t, t1.Value)
	assert.True(t, t2.Value)
	assert.False(t, f.Value)
	assert.NotSameInstance(t, t1, t2)
}

func TestUInt32Value(t *testing.T) {
	v1 := uint32Value(100)
	v2 := uint32Value(100)
	v3 := uint32Value(99)

	assert.Equal(t, v1.Value, uint32(100))
	assert.Equal(t, v2.Value, uint32(100))
	assert.Equal(t, v3.Value, uint32(99))
	assert.NotSameInstance(t, v1, v2)
}

func TestValueString(t *testing.T) {
	s1 := valueString("a")
	s2 := valueString("a")
	s3 := valueString("b")

	assert.Equal(t, s1.GetStringValue(), "a")
	assert.Equal(t, s2.GetStringValue(), "a")
	assert.Equal(t, s3.GetStringValue(), "b")
	assert.NotSameInstance(t, s1, s2)
}

func TestIntPtrToUint32Ptr(t *testing.T) {
	var a, b *types.UInt32Value
	a = intPtrToUint32Ptr(nil)
	assert.Equal(t, a, b)

	a = intPtrToUint32Ptr(ptr.Int(1))
	assert.NotDeepEqual(t, a, b)

	b = &types.UInt32Value{Value: 1}
	assert.DeepEqual(t, a, b)
	assert.DeepEqual(t, a, a)

	b = &types.UInt32Value{Value: 2}
	assert.NotDeepEqual(t, a, b)
}

func TestUint32PtrToIntPtr(t *testing.T) {
	var a, b *int
	a = uint32PtrToIntPtr(nil)
	assert.Equal(t, a, b)

	a = uint32PtrToIntPtr(&types.UInt32Value{Value: 1})
	assert.NotDeepEqual(t, a, b)

	b = ptr.Int(1)
	assert.DeepEqual(t, a, b)
	assert.DeepEqual(t, a, a)

	b = ptr.Int(2)
	assert.NotDeepEqual(t, a, b)
}

func TestIntPtrToDurationPtr(t *testing.T) {
	var a, b *types.Duration
	a = intPtrToDurationPtr(nil)
	assert.Equal(t, a, b)

	a = intPtrToDurationPtr(ptr.Int(20000))
	assert.NotDeepEqual(t, a, b)

	b = &types.Duration{Seconds: 20}
	assert.DeepEqual(t, a, b)

	a = intPtrToDurationPtr(ptr.Int(200123))
	b = &types.Duration{Seconds: 200, Nanos: 123000000}
	assert.DeepEqual(t, a, b)

	a = intPtrToDurationPtr(ptr.Int(0))
	b = &types.Duration{Seconds: 0, Nanos: 0}
	assert.DeepEqual(t, a, b)

	a = intPtrToDurationPtr(ptr.Int(math.MaxInt32))
	b = &types.Duration{Seconds: 2147483, Nanos: 647000000}
	assert.DeepEqual(t, a, b)

	a = intPtrToDurationPtr(ptr.Int(1))
	assert.NotDeepEqual(t, a, b)
}

func TestDurationPtrToIntPtr(t *testing.T) {
	var a, b *int
	a = durationPtrToIntPtr(nil)
	assert.DeepEqual(t, a, b)

	a = durationPtrToIntPtr(&types.Duration{Seconds: math.MaxInt32 * 2})
	assert.Equal(t, a, b)

	a = durationPtrToIntPtr(&types.Duration{Seconds: 1})
	assert.NotDeepEqual(t, a, b)

	b = ptr.Int(1000)
	assert.DeepEqual(t, a, b)

	a = durationPtrToIntPtr(&types.Duration{Seconds: 5000, Nanos: 647123344})
	b = ptr.Int(5000647)
	assert.DeepEqual(t, a, b)

	a = durationPtrToIntPtr(&types.Duration{Seconds: 5000, Nanos: 123344})
	b = ptr.Int(5000000)
	assert.DeepEqual(t, a, b)
}
