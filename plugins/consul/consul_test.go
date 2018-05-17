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

package consul

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	consulapi "github.com/hashicorp/consul/api"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

const testdc = "some-test-dc"

var (
	err  = errors.New("someerrwat")
	opts = &consulapi.QueryOptions{Datacenter: testdc, RequireConsistent: true}
)

func stubGetServices(
	t *testing.T,
	dc string,
	result map[string][]string,
	resultErr error,
) (getConsulServicesFn, func()) {
	called := 0
	fn := func(client catalogInterface, dcIn string) (serviceListing, error) {
		called++
		assert.Equal(t, dcIn, dc)
		assert.NonNil(t, client)
		return result, resultErr
	}

	verify := func() {
		assert.Equal(t, called, 1)
	}

	return fn, verify
}

type stubGetServiceDetail struct {
	svcTag string
	dc     string
	t      *testing.T

	calls    int
	svcCalls map[string]bool
	result   map[string][]interface{}
}

func NewStubGetServiceDetail(t *testing.T, dc, tag string) stubGetServiceDetail {
	return stubGetServiceDetail{
		svcTag:   tag,
		dc:       dc,
		t:        t,
		calls:    0,
		svcCalls: map[string]bool{},
		result:   map[string][]interface{}{},
	}
}

func (s stubGetServiceDetail) addCall(svc string, detail consulServiceDetail, err error) {
	s.result[svc] = []interface{}{detail, err}
}

func (s *stubGetServiceDetail) Fn() getConsulServiceDetailFn {
	return func(client catalogInterface, dc, svcName, svcTag string) (consulServiceDetail, error) {
		s.calls++
		assert.NonNil(s.t, client)
		assert.Equal(s.t, dc, s.dc)
		assert.Equal(s.t, svcTag, s.svcTag)

		if s.svcCalls[svcName] {
			assert.Failed(s.t, fmt.Sprintf("already requested service details for %s", svcName))
		}

		s.svcCalls[svcName] = true
		r, expected := s.result[svcName]
		if expected {
			var fnerr error
			if r[1] != nil {
				fnerr = r[1].(error)
			}
			return r[0].(consulServiceDetail), fnerr
		} else {
			assert.Failed(s.t, fmt.Sprintf("did not expect requset for details of service %s", svcName))
			return consulServiceDetail{}, nil
		}
	}
}

func (s *stubGetServiceDetail) Verify() {
	assert.Equal(s.t, s.calls, len(s.result))
}

type stubGetNodeHealth struct {
	dc string
	t  *testing.T

	calls    int
	svcCalls map[string]bool
	result   map[string][]interface{}
}

func NewStubGetNodeHealth(t *testing.T, dc string) stubGetNodeHealth {
	return stubGetNodeHealth{dc, t, 0, map[string]bool{}, map[string][]interface{}{}}
}

func (s stubGetNodeHealth) addCall(n string, h nodeHealth, e error) {
	s.result[n] = []interface{}{h, e}
}

func (s *stubGetNodeHealth) Fn() getConsulNodeHealthFn {
	return func(client healthInterface, dc, node string) (nodeHealth, error) {
		s.calls++
		assert.NonNil(s.t, client)
		assert.Equal(s.t, dc, s.dc)

		if s.svcCalls[node] {
			assert.Failed(s.t, fmt.Sprintf("called getNodeHealth more than once for %s", node))
		}
		s.svcCalls[node] = true

		r, expected := s.result[node]
		if !expected {
			assert.Failed(s.t, fmt.Sprintf("did not expect requset for details of service %s", node))
		}

		var fnerr error
		if r[1] != nil {
			fnerr = r[1].(error)
		}
		return r[0].(nodeHealth), fnerr
	}
}

func (s *stubGetNodeHealth) Verify() {
	assert.Equal(s.t, s.calls, len(s.result))
}

func NewStubMkClusters(
	t *testing.T,
	hitErr error,
	svcTag string,
	svcDetails map[string]consulServiceDetail,
	health map[string]nodeHealth,
	clusters api.Clusters,
) (mkClusterFn, func()) {
	calls := 0

	fn := func(st string, sd map[string]consulServiceDetail, nh map[string]nodeHealth) api.Clusters {
		if hitErr != nil {
			assert.Failed(
				t,
				fmt.Sprintf("should not have been called having previously hit %v", hitErr))
			return nil
		}
		calls++
		assert.DeepEqual(t, sd, svcDetails)
		assert.DeepEqual(t, nh, health)
		return clusters
	}

	verify := func() {
		if hitErr != nil {
			assert.Equal(t, calls, 0)
		} else {
			assert.Equal(t, calls, 1)
		}
	}

	return fn, verify
}

type testConsulGetClustersCase struct {
	getServicesErr   error
	getSvcDetailErr  error
	getNodeHealthErr error
}

func (tc testConsulGetClustersCase) run(t *testing.T) {
	type tags []string
	type md map[string]string

	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockConsul := newMockConsulClient(ctrl)
	mockHealth := newMockHealthInterface(ctrl)
	mockCatalog := newMockCatalogInterface(ctrl)
	mockConsul.EXPECT().Catalog().Return(mockCatalog)
	mockConsul.EXPECT().Health().Return(mockHealth)

	var hitErr error

	svctag := "tagtagtag"
	svcs := map[string][]string{
		"svca": tags{svctag, "a", "b", "c"},
		"svcb": tags{"a", "b", "c", "b"},
		"svcc": tags{svctag, "a", "b", "c"},
	}

	getSvcsFn, svcsFnVerify := stubGetServices(t, testdc, svcs, tc.getServicesErr)
	defer svcsFnVerify()

	hitErr = tc.getServicesErr

	getSvcDetail := NewStubGetServiceDetail(t, testdc, svctag)
	svcDetails := map[string]consulServiceDetail{
		"svca": {
			"svca",
			[]consulServiceNode{
				{"n1", "ip1", 1, tags{svctag}, md{"d": "e", "f": "g"}},
				{"n2", "ip2", 1, tags{svctag}, md{"h": "i", "j": "k"}},
				{"n3", "ip3", 1, tags{"c"}, md{"l": "m", "n": "o"}},
				{"n4", "ip4", 1, tags{"a", "b"}, md{"p": "q", "r": "s"}},
			},
		},
		"svcc": {
			"svcc",
			[]consulServiceNode{
				{"n1", "ip1", 10, tags{svctag}, md{"d": "e", "f": "g"}},
				{"n2", "ip2", 10, tags{"c"}, md{"h": "i", "j": "k"}},
				{"n3", "ip3", 10, tags{"c"}, md{"l": "m", "n": "o"}},
				{"n4", "ip4", 10, tags{"a", "b"}, md{"p": "q", "r": "s"}},
				{"n5", "ip5", 10, tags{svctag}, md{"t": "u", "v": "w"}},
			},
		},
	}

	sdverifyFn := getSvcDetail.Verify
	if hitErr == nil {
		for k, v := range svcDetails {
			getSvcDetail.addCall(k, v, tc.getSvcDetailErr)
		}

		hitErr = tc.getSvcDetailErr
		if hitErr != nil {
			sdverifyFn = func() {
				assert.Equal(t, getSvcDetail.calls, 1)
			}
		}
	}
	defer sdverifyFn()

	nodeHealth := map[string]nodeHealth{
		"n1": {
			{Node: "n1", CheckID: "n1-c1", Status: "aoeu"},
			{Node: "n1", CheckID: "n1-c2", Status: "aoeu"},
			{Node: "n1", CheckID: "n1-c3", Status: "aoeu"},
			{Node: "n1", CheckID: "n1-c4", Status: "aoeu"},
			{Node: "n1", CheckID: "n1-c5", Status: "aoeu"},
		},
		"n2": {
			{Node: "n2", CheckID: "n2-c1", Status: "aoesunth"},
			{Node: "n2", CheckID: "n2-c2", Status: "aoesunth"},
			{Node: "n2", CheckID: "n2-c3", Status: "aoesunth"},
		},
		"n5": {},
	}
	getNodeHealth := NewStubGetNodeHealth(t, testdc)
	nhVerifyFn := getNodeHealth.Verify
	if hitErr == nil {
		for _, nid := range []string{"n1", "n2", "n5"} {
			getNodeHealth.addCall(nid, nodeHealth[nid], tc.getNodeHealthErr)
		}

		hitErr = tc.getNodeHealthErr
		if hitErr != nil {
			nhVerifyFn = func() {
				assert.Equal(t, getNodeHealth.calls, 1)
			}
		}
	}
	defer nhVerifyFn()

	wantClusters := api.Clusters{
		{ClusterKey: "someasnuthoeus"},
		{ClusterKey: "someasnuthoeus2"},
		{ClusterKey: "someasnuthoeus3"},
	}

	mkClusterFn, mkClusterVerify := NewStubMkClusters(
		t, hitErr, svctag, svcDetails, nodeHealth, wantClusters)
	defer mkClusterVerify()

	gotClusters, err :=
		consulGetClusters(
			mockConsul,
			svctag,
			testdc,
			getSvcsFn,
			getSvcDetail.Fn(),
			getNodeHealth.Fn(),
			mkClusterFn,
		)
	if hitErr == nil {
		assert.ArrayEqual(t, gotClusters, wantClusters)
	} else {
		assert.NonNil(t, err)
	}
}

func TestConsulUpdateGetSvcsErr(t *testing.T) {
	err := errors.New("asonteuh")
	testConsulGetClustersCase{err, nil, nil}.run(t)
	testConsulGetClustersCase{}.run(t)
}
func TestConsulUpdateGetSvcDetailErr(t *testing.T) {
	err := errors.New("asonteuh")
	testConsulGetClustersCase{nil, err, nil}.run(t)
	testConsulGetClustersCase{}.run(t)
}
func TestConsulUpdateGetNodeHealthErr(t *testing.T) {
	err := errors.New("asonteuh")
	testConsulGetClustersCase{nil, nil, err}.run(t)
	testConsulGetClustersCase{}.run(t)
}
func TestConsulUpdate(t *testing.T) {
	testConsulGetClustersCase{}.run(t)
}

func TestMkClusters(t *testing.T) {
	type md map[string]string
	type tags []string

	hc := func(n, c, s, svc string) *consulapi.HealthCheck {
		return &consulapi.HealthCheck{Node: n, CheckID: c, Status: s, ServiceID: svc}
	}

	var (
		tag     = "tagtagtag"
		details = map[string]consulServiceDetail{
			"svc-a": {
				"svc-a",
				[]consulServiceNode{
					{"n2", "ip2", 1, []string{"a-n2-t1", tag, "a-n2-t2"}, md{"a": "b", "c": "d"}},
					{"n3", "ip3", 1, tags{"a-n3-t1", "a-n3-t2", tag}, md{"d": "e", "f": "g"}},
				},
			},
			"svc-b": {
				"svc-b",
				[]consulServiceNode{
					{"n3", "ip3", 3, tags{"b-n3-t1", "b-n3-t2", tag}, md{"l": "m", "n": "o"}},
				},
			},
			"svc-c": {
				"svc-c",
				[]consulServiceNode{},
			},
		}

		pass   = consulapi.HealthPassing
		health = map[string]nodeHealth{
			"n5": {
				hc("n5", "n5-c1", pass, "svc-a"),
				hc("n5", "n5-c2", pass, ""),
			},
			"n1": {hc("n1", "n1-c1", pass, "svc-a")},
			"n3": {
				hc("n3", "n3-c1", pass, "svc-a"),
				hc("n3", "n3-c2", "whee", "svc-b"),
				hc("n3", "n3-c3", pass, ""),
			},
		}

		want = []api.Cluster{
			{
				Name: "svc-a",
				Instances: api.Instances{
					{"ip2", 1, api.MetadataFromMap(md{
						"node-id":     "n2",
						"tag:a-n2-t1": "",
						"tag:a-n2-t2": "",
						"a":           "b",
						"c":           "d",
						"node-health": pass,
					})},
					{"ip3", 1, api.MetadataFromMap(md{
						"node-id":     "n3",
						"tag:a-n3-t1": "",
						"tag:a-n3-t2": "",
						"d":           "e",
						"f":           "g",
						"node-health": pass,
						"check:n3-c1": pass,
						"check:n3-c3": pass,
					})},
				},
			},

			{
				Name: "svc-b",
				Instances: api.Instances{
					{"ip3", 3, api.MetadataFromMap(md{
						"node-id":     "n3",
						"tag:b-n3-t1": "",
						"tag:b-n3-t2": "",
						"l":           "m",
						"n":           "o",
						"check:n3-c2": "whee",
						"check:n3-c3": pass,
						"node-health": "mixed",
					})},
				},
			},

			{Name: "svc-c"},
		}
	)

	got := mkClusters(tag, details, health)
	assert.Equal(t, len(got), 3)

	gotm := got.GroupBy(func(c api.Cluster) string { return c.Name })
	for _, c := range want {
		assert.Equal(t, len(gotm[c.Name]), 1)
		if !assert.True(t, c.Equals(gotm[c.Name][0])) {
			fmt.Printf("got:  %#v\n", gotm[c.Name][0])
			fmt.Printf("want: %#v\n", c)
		}
	}
}

func TestGetConsulDatacentersError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	dcs := []string{"dc1", "dc2"}

	catalog := newMockCatalogInterface(ctrl)
	catalog.EXPECT().Datacenters().Return(dcs, err)

	got, gotErr := getConsulDatacenters(catalog)

	assert.Nil(t, got)
	assert.DeepEqual(t, gotErr, err)
}

func TestGetConsulDatacenters(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	want := []string{"dc1", "dc2"}

	catalog := newMockCatalogInterface(ctrl)
	catalog.EXPECT().Datacenters().Return(want, nil)

	got, gotErr := getConsulDatacenters(catalog)

	assert.Nil(t, gotErr)
	assert.DeepEqual(t, got, want)
}

func TestServiceListingFindWithTag(t *testing.T) {
	tag := "tag"
	in := serviceListing{
		"svca": []string{"a", "b", tag, "c"},
		"svcb": []string{"a", "b", "c"},
		"svcc": []string{"a", "b", "c"},
		"svcd": []string{"a", "b", "c", tag},
		"svce": []string{tag, "a", "b", "c"},
	}

	got := in.FindWithTag(tag)

	assert.DeepEqual(t, got, serviceListing{
		"svca": []string{"a", "b", tag, "c"},
		"svcd": []string{"a", "b", "c", tag},
		"svce": []string{tag, "a", "b", "c"},
	})
}

func TestGetConsulServicesError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	want := map[string][]string{
		"svca": {"tag", "tag2", "tag3"},
		"svcb": {"tag", "tag2", "tag3"},
	}

	catalog := newMockCatalogInterface(ctrl)
	catalog.EXPECT().Services(opts).Return(want, nil, err)

	got, gotErr := getConsulServices(catalog, testdc)
	assert.DeepEqual(t, gotErr, err)
	assert.Nil(t, got)
}

func TestGetConsulServices(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	want := map[string][]string{
		"svca": {"tag", "tag2", "tag3"},
		"svcb": {"tag", "tag2", "tag3"},
	}

	catalog := newMockCatalogInterface(ctrl)
	catalog.EXPECT().Services(opts).Return(want, nil, nil)

	got, gotErr := getConsulServices(catalog, testdc)
	assert.Nil(t, gotErr)
	assert.DeepEqual(t, got, want)
}

func TestConsulServiceNodeHasTag(t *testing.T) {
	n := consulServiceNode{
		tags: []string{"hastag", "a", "b"},
	}

	assert.True(t, n.HasTag("a"))
	assert.False(t, n.HasTag("!a"))
}

func TestServiceDetailFromSvcs(t *testing.T) {
	tags1 := []string{"tag1-a", "tag1-b"}
	meta1 := map[string]string{"a": "b", "c": "d"}
	tags2 := []string{"tag2-a", "tag2-b"}
	meta2 := map[string]string{"l": "m", "n": "o"}

	in := []*consulapi.CatalogService{
		{
			Node:        "n1",
			Address:     "addr1",
			ServiceTags: tags1,
			ServicePort: 1234,
			NodeMeta:    meta1,
		},
		{
			Node:           "n2",
			ServiceAddress: "addr2",
			ServiceTags:    tags2,
			ServicePort:    1234,
			NodeMeta:       meta2,
		},
	}

	svcid := "svcsvcsvc"

	want := consulServiceDetail{
		svcid,
		[]consulServiceNode{
			{"n1", "addr1", 1234, tags1, meta1},
			{"n2", "addr2", 1234, tags2, meta2},
		},
	}

	assert.DeepEqual(t, serviceDetailFromSvcs(svcid, in), want)
}

func TestGetConsulServiceDetailError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	catalog := newMockCatalogInterface(ctrl)
	svcname := "test-svc"
	svctag := "tagtagtag"

	svcs := []*consulapi.CatalogService{
		{Node: "nodeid", Address: "bob", ServicePort: 3},
	}

	catalog.EXPECT().Service(svcname, svctag, opts).Return(svcs, nil, err)

	got, gotErr := getConsulServiceDetail(catalog, testdc, svcname, svctag)
	assert.DeepEqual(t, gotErr, err)
	assert.DeepEqual(t, got, consulServiceDetail{})
}

func TestGetConsulServiceDetail(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	catalog := newMockCatalogInterface(ctrl)
	svcname := "test-svc"
	svctag := "tagtagtag"

	svcs := []*consulapi.CatalogService{
		{Node: "nodeid", Address: "bob", ServicePort: 3},
	}

	catalog.EXPECT().Service(svcname, svctag, opts).Return(svcs, nil, nil)

	got, gotErr := getConsulServiceDetail(catalog, testdc, svcname, svctag)
	assert.Nil(t, gotErr)
	assert.DeepEqual(t, got, serviceDetailFromSvcs(svcname, svcs))
}

func TestGetConsulNodeHealth(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	tgt := "some-node"
	health := []*consulapi.HealthCheck{
		{Node: tgt, Status: "passing"},
		{Node: tgt, Status: "critical"},
	}

	healthApi := newMockHealthInterface(ctrl)
	healthApi.EXPECT().Node(tgt, opts).Return(health, nil, nil)

	got, gotErr := getConsulNodeHealth(healthApi, testdc, tgt)
	assert.Nil(t, gotErr)
	assert.DeepEqual(t, got, health)
}

func TestGetConsulNodeHealthError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	tgt := "some-node"
	health := []*consulapi.HealthCheck{
		{Node: tgt, Status: "passing"},
		{Node: tgt, Status: "critical"},
	}

	healthApi := newMockHealthInterface(ctrl)
	healthApi.EXPECT().Node(tgt, opts).Return(health, nil, err)

	got, gotErr := getConsulNodeHealth(healthApi, testdc, tgt)
	assert.DeepEqual(t, gotErr, err)
	assert.Nil(t, got)
}
