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

package aws

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/test/assert"
)

var boom = errors.New("boooooom")

func randStr(l int) string {
	src := "0123456789-_abcdefghijklmnopqrstuvwxyz"
	ls := len(src)
	s := ""
	for i := 0; i < l; i++ {
		s += string(src[rand.Intn(ls)])
	}

	return s
}

func TestNewAwsClientSliceSizes(t *testing.T) {
	awsClient := newAwsClient(nil, nil)
	c, ok := awsClient.(awsAdapter)

	assert.True(t, ok)
	assert.Equal(t, c.describeServicesWindowSz, 10)
	assert.Equal(t, c.describeTasksWindowSz, 100)
}

var sliceWalkInput = ptr.StringSlice([]string{"one", "two", "three", "four", "five", "six", "seven"})

func TestSliceWalkFull(t *testing.T) {
	seen := [][]*string{}

	assert.Nil(t,
		sliceWalk(3, sliceWalkInput, func(in []*string) error {
			seen = append(seen, in)
			return nil
		}),
	)

	assert.DeepEqual(t, seen, [][]*string{
		ptr.StringSlice([]string{"one", "two", "three"}),
		ptr.StringSlice([]string{"four", "five", "six"}),
		ptr.StringSlice([]string{"seven"}),
	})
}

func TestSliceWalkAbort(t *testing.T) {
	seen := [][]*string{}
	i := 0

	assert.DeepEqual(t,
		sliceWalk(3, sliceWalkInput, func(in []*string) error {
			i++
			seen = append(seen, in)

			if i == 2 {
				return boom
			}
			return nil
		}),
		boom,
	)

	assert.DeepEqual(t, seen, [][]*string{
		ptr.StringSlice([]string{"one", "two", "three"}),
		ptr.StringSlice([]string{"four", "five", "six"}),
	})
}

func TestSliceWalkLarger(t *testing.T) {
	seen := [][]*string{}

	assert.Nil(t,
		sliceWalk(1000, sliceWalkInput, func(in []*string) error {
			seen = append(seen, in)
			return nil
		}),
	)

	assert.DeepEqual(
		t,
		seen,
		[][]*string{
			ptr.StringSlice([]string{"one", "two", "three", "four", "five", "six", "seven"}),
		},
	)
}

func TestSliceWalkBadWindowZero(t *testing.T) {
	assert.DeepEqual(t, sliceWalk(0, sliceWalkInput, nil), badWindowSize)
}

func TestSliceWalkBadWindowNegative(t *testing.T) {
	assert.DeepEqual(t, sliceWalk(-1, sliceWalkInput, nil), badWindowSize)
}

func mkAwsAdapter(t *testing.T) (awsAdapter, *mockEcsInterface, func()) {
	ctrl := gomock.NewController(assert.Tracing(t))
	ecs := newMockEcsInterface(ctrl)
	aws := awsAdapter{ecs, newMockEc2Interface(ctrl), 10, 100}
	return aws, ecs, ctrl.Finish
}

func checkReturns(
	t *testing.T,
	got interface{},
	gotErr error,
	want interface{},
	wantErr error,
	wantGotErr ...interface{},
) {
	if wantErr != nil {
		if len(wantGotErr) != 0 {
			assert.DeepEqual(t, got, wantGotErr[0])
		} else {
			assert.Nil(t, got)
		}
		assert.ErrorContains(t, gotErr, wantErr.Error())
	} else {
		assert.DeepEqual(t, got, want)
		assert.Nil(t, gotErr)
	}
}

var failures = []*ecs.Failure{
	{Arn: ptr.String("somearn"), Reason: ptr.String("reason")},
	{Arn: ptr.String("somearn2"), Reason: ptr.String("reason2")},
}

func testGetEC2Instances(t *testing.T, wantErr error) {
	mkInst := func(id string) *ec2.Instance {
		return &ec2.Instance{
			InstanceId: &id,
			ImageId:    ptr.String(randStr(10)),
		}
	}

	aws, _, fin := mkAwsAdapter(t)
	defer fin()

	mock := aws.ec2.(*mockEc2Interface)

	token := ptr.String("token")

	ids := []string{"a", "b", "c", "d"}

	arg1 := ec2.DescribeInstancesInput{
		InstanceIds: ptr.StringSlice(ids),
	}
	arg2 := arg1
	arg2.NextToken = token

	instA := mkInst("a")
	instB := mkInst("b")
	instC := mkInst("c")
	instD := mkInst("d")

	ans1 := &ec2.DescribeInstancesOutput{
		NextToken: token,
		Reservations: []*ec2.Reservation{
			{Instances: []*ec2.Instance{instB}},
			{Instances: []*ec2.Instance{instC}},
		},
	}

	ans2 := &ec2.DescribeInstancesOutput{
		NextToken: nil,
		Reservations: []*ec2.Reservation{
			{Instances: []*ec2.Instance{instA, instD}},
		},
	}

	gomock.InOrder(
		mock.EXPECT().DescribeInstances(&arg1).Return(ans1, nil),
		mock.EXPECT().DescribeInstances(&arg2).Return(ans2, wantErr),
	)

	got, gotErr := aws.GetEC2Instances(ids...)

	want := map[string]ec2Instance{
		"a": {instA},
		"b": {instB},
		"c": {instC},
		"d": {instD},
	}

	if wantErr != nil {
		want = nil
	}

	checkReturns(t, got, gotErr, want, wantErr)
}

func TestGetEC2Instances(t *testing.T) {
	testGetEC2Instances(t, nil)
}

func TestGetEC2InstancesError(t *testing.T) {
	testGetEC2Instances(t, boom)
}

func doTestListClusters(t *testing.T, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	token := ptr.String("token")
	arg1 := ecs.ListClustersInput{}
	arg2 := arg1
	arg2.NextToken = token

	arns1 := []string{"cluster/a", "cluster/b", "cluster/c"}
	arns2 := []string{"cluster/d"}

	ans1 := &ecs.ListClustersOutput{
		ClusterArns: ptr.StringSlice(arns1),
		NextToken:   token,
	}
	ans2 := &ecs.ListClustersOutput{
		ClusterArns: ptr.StringSlice(arns2),
		NextToken:   nil,
	}

	gomock.InOrder(
		mock.EXPECT().ListClusters(&arg1).Return(ans1, nil),
		mock.EXPECT().ListClusters(&arg2).Return(ans2, wantErr),
	)

	got, gotErr := aws.ListClusters()

	want := map[string]arn{
		"a": arn("cluster/a"),
		"b": arn("cluster/b"),
		"c": arn("cluster/c"),
		"d": arn("cluster/d"),
	}

	checkReturns(t, got, gotErr, want, wantErr)
}

func TestListClusters(t *testing.T) {
	doTestListClusters(t, nil)
}

func TestListClustersError(t *testing.T) {
	doTestListClusters(t, boom)
}

func doTestListServices(t *testing.T, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	c := "cluster"

	token := ptr.String("token")
	arg1 := ecs.ListServicesInput{Cluster: &c}
	arg2 := ecs.ListServicesInput{Cluster: &c, NextToken: token}

	arna := "cluster/svc/a"
	arnb := "cluster/svc/b"
	arnc := "cluster/svc/c"
	arnd := "cluster/svc/d"

	ans1 := &ecs.ListServicesOutput{
		ServiceArns: ptr.StringSlice([]string{arna, arnb}),
		NextToken:   token,
	}
	ans2 := &ecs.ListServicesOutput{ServiceArns: ptr.StringSlice([]string{arnc, arnd})}

	gomock.InOrder(
		mock.EXPECT().ListServices(&arg1).Return(ans1, nil),
		mock.EXPECT().ListServices(&arg2).Return(ans2, wantErr),
	)

	got, gotErr := aws.ListServices(c)

	want := map[string]arn{
		"a": arn(arna),
		"b": arn(arnb),
		"c": arn(arnc),
		"d": arn(arnd),
	}

	checkReturns(t, got, gotErr, want, wantErr)
}

func TestListServices(t *testing.T) {
	doTestListServices(t, nil)
}

func TestListServicesError(t *testing.T) {
	doTestListServices(t, boom)
}

func doTestListTasks(t *testing.T, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	c := ptr.String("cluster")
	svcName := ptr.String("svcsvc")
	token := ptr.String("token")

	tarna := "cluster/svcsvc/task-a"
	tarnb := "cluster/svcsvc/task-b"
	tarnc := "cluster/svcsvc/task-c"
	tarnd := "cluster/svcsvc/task-d"

	arg1 := &ecs.ListTasksInput{Cluster: c, ServiceName: svcName}
	arg2 := &ecs.ListTasksInput{Cluster: c, ServiceName: svcName, NextToken: token}
	ans1 := &ecs.ListTasksOutput{NextToken: token, TaskArns: ptr.StringSlice([]string{tarna, tarnb})}
	ans2 := &ecs.ListTasksOutput{TaskArns: ptr.StringSlice([]string{tarnc, tarnd})}

	gomock.InOrder(
		mock.EXPECT().ListTasks(arg1).Return(ans1, nil),
		mock.EXPECT().ListTasks(arg2).Return(ans2, wantErr),
	)

	got, gotErr := aws.ListTasks(*c, arn(*svcName))

	want := []arn{arn(tarna), arn(tarnb), arn(tarnc), arn(tarnd)}

	checkReturns(t, got, gotErr, want, wantErr)
}

func TestListTasks(t *testing.T) {
	doTestListTasks(t, nil)
}

func TestListTasksError(t *testing.T) {
	doTestListTasks(t, boom)
}

func doTestServiceDefinitions(t *testing.T, failures []*ecs.Failure, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	aws.describeServicesWindowSz = 2

	mkSvc := func(id string) *ecs.Service {
		return &ecs.Service{
			ServiceArn:  &id,
			ServiceName: ptr.String(randStr(20)),
		}
	}

	c := "cluster"
	arna := "arn/svca"
	arnb := "arn/svcb"
	arnc := "arn/svcc"
	arnd := "arn/svcd"
	arne := "arn/svce"
	svca := mkSvc(arna)
	svcb := mkSvc(arnb)
	svcc := mkSvc(arnc)
	svcd := mkSvc(arnd)
	svce := mkSvc(arne)

	allSvcs := [][]string{
		{"svca", "svcb"},
		{"svcc", "svcd"},
		{"svce"},
	}

	resp := [][]*ecs.Service{
		{svca, svcb},
		{svcc, svcd},
		{svce},
	}

	calls := []*gomock.Call{}
	addCall := func(c *gomock.Call) { calls = append(calls, c) }

	for i, svcs := range allSvcs {
		arg := &ecs.DescribeServicesInput{
			Cluster:  &c,
			Services: ptr.StringSlice(svcs),
		}

		ans := &ecs.DescribeServicesOutput{
			Failures: failures,
			Services: resp[i],
		}

		addCall(mock.EXPECT().DescribeServices(arg).Return(ans, wantErr))
		if wantErr != nil {
			break
		}
	}

	got, gotErr := aws.ServiceDefinitions(c, s2a([]string{"svca", "svcb", "svcc", "svcd", "svce"}))

	want := map[arn]svcDefn{
		arn(arna): {svca},
		arn(arnb): {svcb},
		arn(arnc): {svcc},
		arn(arnd): {svcd},
		arn(arne): {svce},
	}
	checkReturns(t, got, gotErr, want, wantErr)
	gomock.InOrder(calls...)
}

func TestServiceDefinitions(t *testing.T) {
	doTestServiceDefinitions(t, nil, nil)
}

func TestServiceDefinitionsError(t *testing.T) {
	doTestServiceDefinitions(t, nil, boom)
}

func TestServiceDefinitionsFailures(t *testing.T) {
	doTestServiceDefinitions(t, failures, nil)
}

func doTestTaskDefinition(t *testing.T, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	tarn := "arn:task/arn"
	arg := &ecs.DescribeTaskDefinitionInput{TaskDefinition: &tarn}
	td := &ecs.TaskDefinition{TaskDefinitionArn: &tarn}
	ans := &ecs.DescribeTaskDefinitionOutput{TaskDefinition: td}

	mock.EXPECT().DescribeTaskDefinition(arg).Return(ans, wantErr)
	got, gotErr := aws.TaskDefinition(arn(tarn))

	want := taskDefn{td}

	checkReturns(t, got, gotErr, want, wantErr, taskDefn{})
}

func TestTaskDefinition(t *testing.T) {
	doTestTaskDefinition(t, nil)
}

func TestTaskDefinitionError(t *testing.T) {
	doTestTaskDefinition(t, boom)
}

func doTestGetTasks(t *testing.T, failures []*ecs.Failure, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	// make this small to force slicing
	aws.describeTasksWindowSz = 3

	mkTask := func(id arn) *ecs.Task {
		return &ecs.Task{TaskArn: ptr.String(string(id)), LastStatus: ptr.String(randStr(20))}
	}

	tarna := arn("arn:task/a")
	taska := mkTask(tarna)
	tarnb := arn("arn:task/b")
	taskb := mkTask(tarnb)
	tarnc := arn("arn:task/c")
	taskc := mkTask(tarnc)
	tarnd := arn("arn:task/d")
	taskd := mkTask(tarnd)
	tarne := arn("arn:task/e")
	taske := mkTask(tarne)
	tarnf := arn("arn:task/f")
	taskf := mkTask(tarnf)

	cluster := "cluster"
	allTarns := []arn{tarna, tarnb, tarnc, tarnd, tarne, tarnf}
	tarnsAry := [][]arn{
		{tarna, tarnb, tarnc},
		{tarne, tarnf},
	}
	tarnsResultsAry := [][]*ecs.Task{
		{taska, taskb, taskc},
		{taske, taskf},
	}

	calls := []*gomock.Call{}
	addCall := func(c *gomock.Call) {
		calls = append(calls, c)
	}

	for i, tarns := range tarnsAry {
		arg := &ecs.DescribeTasksInput{Cluster: ptr.String(cluster), Tasks: ptr.StringSlice(a2s(tarns))}
		ans := &ecs.DescribeTasksOutput{
			Failures: failures,
			Tasks:    tarnsResultsAry[i],
		}

		addCall(mock.EXPECT().DescribeTasks(arg).Return(ans, wantErr))
		if wantErr != nil {
			break
		}
	}

	gomock.InOrder(calls...)

	start := map[arn]taskInst{tarnd: {taskd}}
	startState := map[arn]taskInst{tarnd: {taskd}}
	got, gotErr := aws.GetTasks(startState, cluster, allTarns...)

	want := map[arn]taskInst{
		tarna: {taska},
		tarnb: {taskb},
		tarnc: {taskc},
		tarne: {taske},
		tarnf: {taskf},
	}

	checkReturns(t, got, gotErr, want, wantErr)
	assert.DeepEqual(t, startState, start)
}

func TestGetTasks(t *testing.T) {
	doTestGetTasks(t, nil, nil)
}

func TestGetTasksFailures(t *testing.T) {
	doTestGetTasks(t, failures, nil)
}

func TestGetTasksError(t *testing.T) {
	doTestGetTasks(t, nil, boom)
}

func doTestGetContainerInstances(t *testing.T, failures []*ecs.Failure, wantErr error) {
	aws, mock, fin := mkAwsAdapter(t)
	defer fin()

	mkInst := func(id arn) *ecs.ContainerInstance {
		return &ecs.ContainerInstance{
			ContainerInstanceArn: ptr.String(string(id)),
			Ec2InstanceId:        ptr.String(randStr(20)),
		}
	}

	cluster := "cluster"

	ciarna := arn("arn:task/ciarna")
	insta := mkInst(ciarna)
	ciarnb := arn("arn:task/ciarnb")
	instb := mkInst(ciarnb)
	ciarnc := arn("arn:task/ciarnc")
	instc := mkInst(ciarnc)

	ciarns := []arn{ciarna, ciarnb}
	allciarns := append(ciarns, ciarnc)

	arg := &ecs.DescribeContainerInstancesInput{
		Cluster:            &cluster,
		ContainerInstances: ptr.StringSlice(a2s(ciarns)),
	}

	ans := &ecs.DescribeContainerInstancesOutput{
		Failures:           failures,
		ContainerInstances: []*ecs.ContainerInstance{insta, instb},
	}

	start := map[arn]containerInst{ciarnc: {instc}}
	startState := map[arn]containerInst{ciarnc: {instc}}

	mock.EXPECT().DescribeContainerInstances(arg).Return(ans, wantErr)
	got, gotErr := aws.GetContainerInstances(startState, cluster, allciarns...)

	want := map[arn]containerInst{
		ciarna: {insta},
		ciarnb: {instb},
	}

	checkReturns(t, got, gotErr, want, wantErr)
	assert.DeepEqual(t, startState, start)
}

func TestGetContainerInstances(t *testing.T) {
	doTestGetContainerInstances(t, nil, nil)
}

func TestGetContainerInstancesFailure(t *testing.T) {
	doTestGetContainerInstances(t, failures, nil)
}

func TestGetContainerInstancesError(t *testing.T) {
	doTestGetContainerInstances(t, nil, boom)
}
