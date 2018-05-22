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
	"time"

	"github.com/golang/mock/gomock"
	consulapi "github.com/hashicorp/consul/api"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
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

func newStubGetServiceDetail(t *testing.T, dc, tag string) stubGetServiceDetail {
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
		}

		assert.Failed(
			s.t,
			fmt.Sprintf("did not expect requset for details of service %s", svcName),
		)
		return consulServiceDetail{}, nil
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

func newStubGetNodeHealth(t *testing.T, dc string) stubGetNodeHealth {
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
			assert.Failed(
				s.t,
				fmt.Sprintf("did not expect requset for details of service %s", node),
			)
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

func newStubMkClusters(
	t *testing.T,
	hitErr error,
	svcTag string,
	svcDetails map[string]consulServiceDetail,
	health map[string]nodeHealth,
	clusters api.Clusters,
) (mkClusterFn, func()) {
	calls := 0

	fn := func(
		st string,
		sd map[string]consulServiceDetail,
		nh map[string]nodeHealth,
	) api.Clusters {
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

	getSvcDetail := newStubGetServiceDetail(t, testdc, svctag)
	svcDetails := map[string]consulServiceDetail{
		"svca": {
			"svca",
			[]consulServiceNode{
				{
					id:       "n1",
					address:  "ip1",
					port:     1,
					tags:     tags{svctag},
					nodeMeta: md{"d": "e", "f": "g"},
				},
				{
					id:       "n2",
					address:  "ip2",
					port:     1,
					tags:     tags{svctag},
					nodeMeta: md{"h": "i", "j": "k"},
				},
				{
					id:       "n3",
					address:  "ip3",
					port:     1,
					tags:     tags{"c"},
					nodeMeta: md{"l": "m", "n": "o"},
				},
				{
					id:       "n4",
					address:  "ip4",
					port:     1,
					tags:     tags{"a", "b"},
					nodeMeta: md{"p": "q", "r": "s"},
				},
			},
		},
		"svcc": {
			"svcc",
			[]consulServiceNode{
				{
					id:       "n1",
					address:  "ip1",
					port:     10,
					tags:     tags{svctag},
					nodeMeta: md{"d": "e", "f": "g"},
				},
				{
					id:       "n2",
					address:  "ip2",
					port:     10,
					tags:     tags{"c"},
					nodeMeta: md{"h": "i", "j": "k"},
				},
				{
					id:       "n3",
					address:  "ip3",
					port:     10,
					tags:     tags{"c"},
					nodeMeta: md{"l": "m", "n": "o"},
				},
				{
					id:       "n4",
					address:  "ip4",
					port:     10,
					tags:     tags{"a", "b"},
					nodeMeta: md{"p": "q", "r": "s"},
				},
				{
					id:       "n5",
					address:  "ip5",
					port:     10,
					tags:     tags{svctag},
					nodeMeta: md{"t": "u", "v": "w"},
				},
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
	getNodeHealth := newStubGetNodeHealth(t, testdc)
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

	mkClusterFn, mkClusterVerify := newStubMkClusters(
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

func testMkClustersWithDelimeter(t *testing.T, delim string) {
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
					{
						id:      "n2",
						address: "ip2",
						port:    1,
						tags: tags{
							fmt.Sprintf("an2t1%sv1", delim),
							fmt.Sprintf("an2t2%sv2", delim),
							fmt.Sprintf("an2t2v1%s", delim),
							tag,
						},
						nodeMeta:    md{"a": "b", "c": "d"},
						serviceMeta: md{"h": "i", "j": "k"},
					},
					{
						id:      "n3",
						address: "ip3",
						port:    1,
						tags: tags{
							fmt.Sprintf("an3t1%sv1", delim),
							fmt.Sprintf("an3t2%sv2", delim),
							fmt.Sprintf("%san3t2v1", delim),
							tag,
						},
						nodeMeta:    md{"d": "e", "f": "g"},
						serviceMeta: md{"p": "q", "r": "s"},
					},
				},
			},
			"svc-b": {
				"svc-b",
				[]consulServiceNode{
					{
						id:      "n3",
						address: "ip3",
						port:    3,
						tags: tags{
							fmt.Sprintf("bn3t1%s", delim),
							fmt.Sprintf("bn3t2%s", delim),
							tag,
						},
						nodeMeta:    md{"l": "m", "n": "o"},
						serviceMeta: md{"t": "u", "v": "w"},
					},
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
					{
						Host: "ip2",
						Port: 1,
						Metadata: api.MetadataFromMap(
							md{
								"node-id":     "n2",
								"tag:an2t1":   "v1",
								"tag:an2t2":   "v2",
								"tag:an2t2v1": "",
								"node:a":      "b",
								"node:c":      "d",
								"h":           "i",
								"j":           "k",
								"node-health": pass,
							},
						),
					},
					{
						Host: "ip3",
						Port: 1,
						Metadata: api.MetadataFromMap(
							md{
								"node-id":     "n3",
								"tag:an3t1":   "v1",
								"tag:an3t2":   "v2",
								"node:d":      "e",
								"node:f":      "g",
								"node-health": pass,
								"check:n3-c1": pass,
								"check:n3-c3": pass,
								"p":           "q",
								"r":           "s",
							},
						),
					},
				},
			},
			{
				Name: "svc-b",
				Instances: api.Instances{
					{
						Host: "ip3",
						Port: 3,
						Metadata: api.MetadataFromMap(
							md{
								"node-id":     "n3",
								"tag:bn3t1":   "",
								"tag:bn3t2":   "",
								"node:l":      "m",
								"node:n":      "o",
								"check:n3-c2": "whee",
								"check:n3-c3": pass,
								"node-health": "mixed",
								"t":           "u",
								"v":           "w",
							},
						),
					},
				},
			},

			{Name: "svc-c"},
		}
	)

	got := getMkClusterFn(delimiterTagParser(delim))(tag, details, health)
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

func TestMkClustersWithColonDelimiter(t *testing.T) {
	testMkClustersWithDelimeter(t, ":")
}

func TestMkClustersWithPipeDelimiter(t *testing.T) {
	testMkClustersWithDelimeter(t, "|")
}

func TestPassThroughTagParser(t *testing.T) {
	a, b, e := passThroughTagParser("a")
	assert.Nil(t, e)
	assert.Equal(t, a, "tag:a")
	assert.Equal(t, b, "")

	a, b, e = passThroughTagParser("")
	assert.Nil(t, e)
	assert.Equal(t, a, "tag:")
	assert.Equal(t, b, "")
}

func TestDelimiterTagParser(t *testing.T) {
	p := delimiterTagParser(":")

	a, b, e := p("")
	assert.NonNil(t, e)
	assert.ErrorContains(t, e, "Invalid delimiter position")
	assert.Equal(t, a, "")
	assert.Equal(t, b, "")

	a, b, e = p("ab:")
	assert.Nil(t, e)
	assert.Equal(t, a, "tag:ab")
	assert.Equal(t, b, "")

	a, b, e = p("a:b")
	assert.Nil(t, e)
	assert.Equal(t, a, "tag:a")
	assert.Equal(t, b, "b")

	a, b, e = p(":ab")
	assert.NonNil(t, e)
	assert.ErrorContains(t, e, "Invalid delimiter position")
	assert.Equal(t, a, "")
	assert.Equal(t, b, "")

	a, b, e = p("ab")
	assert.Nil(t, e)
	assert.Equal(t, a, "tag:ab")
	assert.Equal(t, b, "")
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
			{
				id:       "n1",
				address:  "addr1",
				port:     1234,
				tags:     tags1,
				nodeMeta: meta1,
			},
			{
				id:       "n2",
				address:  "addr2",
				port:     1234,
				tags:     tags2,
				nodeMeta: meta2,
			},
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

	healthAPI := newMockHealthInterface(ctrl)
	healthAPI.EXPECT().Node(tgt, opts).Return(health, nil, nil)

	got, gotErr := getConsulNodeHealth(healthAPI, testdc, tgt)
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

	healthAPI := newMockHealthInterface(ctrl)
	healthAPI.EXPECT().Node(tgt, opts).Return(health, nil, err)

	got, gotErr := getConsulNodeHealth(healthAPI, testdc, tgt)
	assert.DeepEqual(t, gotErr, err)
	assert.Nil(t, got)
}

func TestCmd(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	assert.SameInstance(t, r.updaterFlags, mockUpdaterFromFlags)
	assert.NonNil(t, r.endpoint)
}

func TestConsulRunnerRun(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	sync := make(chan struct{}, 1)

	mockUpdater := updater.NewMockUpdater(ctrl)
	mockUpdater.EXPECT().Delay().Return(time.Minute)
	mockUpdater.EXPECT().Close().Return(nil)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	mockConsulHealth := newMockHealthInterface(ctrl)

	mockConsulCatalog := newMockCatalogInterface(ctrl)
	mockConsulCatalog.EXPECT().Datacenters().Return([]string{"dc"}, nil)
	mockConsulCatalog.EXPECT().
		Services(gomock.Any()).
		Return(nil, nil, errors.New("boom")).
		Do(func(_ *consulapi.QueryOptions) { sync <- struct{}{} })

	mockConsulClient := newMockConsulClient(ctrl)
	mockConsulClient.EXPECT().Catalog().Return(mockConsulCatalog).AnyTimes()
	mockConsulClient.EXPECT().Health().Return(mockConsulHealth)

	mockGetClient := newMockGetClientInterface(ctrl)
	mockGetClient.EXPECT().getClient().Return(mockConsulClient, nil)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.endpoint = mockGetClient
	r.consulSettings.consulDC = "dc"

	result := make(chan command.CmdErr, 1)
	go func() {
		result <- r.Run(cmd, nil)
	}()

	<-sync

	updater.StopLoop()
	assert.Equal(t, <-result, command.NoError())
}

func TestConsulRunnerRunNoDC(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "datacenter must be specified")
}

func TestConsulRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(errors.New("bad updater flags"))

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.consulDC = "dc"

	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "bad updater flags")
}

func TestConsulRunnerRunUpdaterMakeError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(nil, errors.New("updater failed"))

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.consulDC = "dc"

	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "updater failed")
}

func TestConsulRunnerRunConsulClientError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	mockGetClient := newMockGetClientInterface(ctrl)
	mockGetClient.EXPECT().getClient().Return(nil, errors.New("consul client error"))

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.endpoint = mockGetClient
	r.consulSettings.consulDC = "dc"

	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "consul client error")
}

func TestConsulRunnerRunConsulDatacentersError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	mockConsulCatalog := newMockCatalogInterface(ctrl)
	mockConsulCatalog.EXPECT().Datacenters().Return(nil, errors.New("datacenters lookup error"))

	mockConsulClient := newMockConsulClient(ctrl)
	mockConsulClient.EXPECT().Catalog().Return(mockConsulCatalog).AnyTimes()

	mockGetClient := newMockGetClientInterface(ctrl)
	mockGetClient.EXPECT().getClient().Return(mockConsulClient, nil)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.endpoint = mockGetClient
	r.consulSettings.consulDC = "dc"

	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "datacenters lookup error")
}

func TestConsulRunnerRunConsulDatacenterNotFound(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	mockConsulCatalog := newMockCatalogInterface(ctrl)
	mockConsulCatalog.EXPECT().Datacenters().Return([]string{"dc"}, nil)

	mockConsulClient := newMockConsulClient(ctrl)
	mockConsulClient.EXPECT().Catalog().Return(mockConsulCatalog).AnyTimes()

	mockGetClient := newMockGetClientInterface(ctrl)
	mockGetClient.EXPECT().getClient().Return(mockConsulClient, nil)

	cmd := Cmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})

	r := cmd.Runner.(*consulRunner)
	r.consulSettings.endpoint = mockGetClient
	r.consulSettings.consulDC = "other-dc"

	cmdErr := r.Run(cmd, nil)
	assert.StringContains(t, cmdErr.Message, "Datacenter other-dc was not found")
}
