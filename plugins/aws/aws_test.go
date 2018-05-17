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
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"
)

func mkAwsCollector() awsCollector {
	return awsCollector{
		settings: awsCollectorSettings{
			namespace: "tbn:cluster",
			delimiter: ":",
		},
	}
}

func TestAWSCmd(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	cmd := AWSCmd(mockUpdaterFromFlags)
	cmd.Flags.Parse([]string{})
	runner := cmd.Runner.(*awsRunner)
	assert.NonNil(t, runner.awsFlags)
	assert.Equal(t, runner.updaterFlags, mockUpdaterFromFlags)
}

func TestAWSRunnerRun(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdater := updater.NewMockUpdater(ctrl)
	mockUpdater.EXPECT().Delay().Return(time.Minute)
	mockUpdater.EXPECT().Close().Return(nil)

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(mockUpdater, nil)

	sync := make(chan struct{}, 1)

	mockEC2Client := newMockEc2Interface(ctrl)
	mockEC2Client.EXPECT().
		DescribeInstances(
			&ec2.DescribeInstancesInput{
				Filters: []*ec2.Filter{
					{
						Name:   aws.String("instance-state-name"),
						Values: []*string{aws.String("running")},
					},
					{
						Name:   aws.String("vpc-id"),
						Values: []*string{aws.String("vpc")},
					},
				},
			},
		).
		Return(nil, errors.New("boom")).
		Do(func(_ *ec2.DescribeInstancesInput) {
			sync <- struct{}{}
		})

	mockClientFromFlags := newMockClientFromFlags(ctrl)
	mockClientFromFlags.EXPECT().MakeEC2Client().Return(mockEC2Client)

	cmd := AWSCmd(mockUpdaterFromFlags)
	r := cmd.Runner.(*awsRunner)
	r.awsFlags = mockClientFromFlags
	r.settings.vpcID = "vpc"

	result := make(chan command.CmdErr, 1)
	go func() {
		result <- r.Run(cmd, nil)
	}()

	<-sync

	updater.StopLoop()
	assert.Equal(t, <-result, command.NoError())
}

func TestAWSRunnerProcessFilters(t *testing.T) {
	r := awsRunner{}
	got, err := r.processFilters(nil)
	assert.Nil(t, err)
	assert.Equal(t, len(got), 0)

	got, err = r.processFilters([]string{"foo"})
	assert.NonNil(t, err)
	assert.Nil(t, got)

	want := map[string][]string{
		"foo": {"bar"},
	}
	got, err = r.processFilters([]string{"foo=bar"})
	assert.Nil(t, err)
	assert.DeepEqual(t, got, want)

	want = map[string][]string{
		"foo":   {"bar", "baz"},
		"rando": {" rondo=rhino"},
	}
	got, err = r.processFilters([]string{"foo=bar", "foo=baz", "rando= rondo=rhino"})
	assert.Nil(t, err)
	assert.DeepEqual(t, got, want)
}

func TestAWSRunnerRunBadUpdaterFlags(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	ar := awsRunner{updaterFlags: mockUpdaterFromFlags}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(err)

	cmdErr := ar.Run(AWSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "aws: "+err.Error())
}

func TestAWSRunnerRunMakeUpdaterError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockUpdaterFromFlags := rotor.NewMockUpdaterFromFlags(ctrl)

	ar := awsRunner{updaterFlags: mockUpdaterFromFlags}

	err := errors.New("boom")
	mockUpdaterFromFlags.EXPECT().Validate().Return(nil)
	mockUpdaterFromFlags.EXPECT().Make().Return(nil, err)

	cmdErr := ar.Run(AWSCmd(mockUpdaterFromFlags), nil)
	assert.Equal(t, cmdErr.Message, "aws: "+err.Error())
}

func TestAWSCollectorMkFilters(t *testing.T) {
	c := mkAwsCollector()
	c.settings.filters = map[string][]string{
		"foo": {"bar", "baz"},
		"1":   {"2"},
	}

	wantFilters := []*ec2.Filter{
		{
			Name:   aws.String("instance-state-name"),
			Values: []*string{aws.String("running")},
		},
		{
			Name:   aws.String("vpc-id"),
			Values: []*string{aws.String(c.settings.vpcID)},
		},
		{
			Name:   aws.String("foo"),
			Values: []*string{aws.String("bar"), aws.String("baz")},
		},
		{
			Name:   aws.String("1"),
			Values: []*string{aws.String("2")},
		},
	}

	assert.HasSameElements(t, c.mkFilters(), wantFilters)
}

func TestAWSCollectorProcessEC2InstanceTwoClustersExistingCluster(t *testing.T) {
	inst := &ec2.Instance{
		PrivateIpAddress: aws.String("1.2.3.4"),
		Tags: []*ec2.Tag{
			{Key: aws.String("rando"), Value: aws.String("rondo")},
			{Key: aws.String("tbn:cluster:c1:80"), Value: aws.String("")},
			{Key: aws.String("tbn:cluster:c1:80:k1"), Value: aws.String("v11")},
			{Key: aws.String("tbn:cluster:c1:80:k2:wat"), Value: aws.String("v21")},
			{Key: aws.String("tbn:cluster:c2:443:k1"), Value: aws.String("v12")},
			{Key: aws.String("tbn:cluster:c2:443:k2:wat:bro"), Value: aws.String("v22")},
		},
	}

	c := mkAwsCollector()

	clusters := map[string]*api.Cluster{
		"c1": {
			Name: "c1",
			Instances: api.Instances{
				{
					Host: "2.3.4.5",
					Port: 8080,
				},
			},
		},
	}
	c.processEC2Instance(clusters, inst)

	wantClusters := map[string]*api.Cluster{
		"c1": {
			Name: "c1",
			Instances: api.Instances{
				{
					Host: "1.2.3.4",
					Port: 80,
					Metadata: api.Metadata{
						{Key: "k1", Value: "v11"},
						{Key: "k2:wat", Value: "v21"},
						{Key: "rando", Value: "rondo"},
					},
				},
				{
					Host: "2.3.4.5",
					Port: 8080,
				},
			},
		},
		"c2": {
			Name: "c2",
			Instances: api.Instances{
				{
					Host: "1.2.3.4",
					Port: 443,
					Metadata: api.Metadata{
						{Key: "k1", Value: "v12"},
						{Key: "k2:wat:bro", Value: "v22"},
						{Key: "rando", Value: "rondo"},
					},
				},
			},
		},
	}
	assert.Equal(t, len(clusters), len(wantClusters))
	assert.DeepEqual(t, clusters["c2"], wantClusters["c2"])
	assert.HasSameElements(t, clusters["c1"].Instances, wantClusters["c1"].Instances)
}

func TestAWSCollectorProcessEC2InstanceNoClusters(t *testing.T) {
	inst := &ec2.Instance{
		PrivateIpAddress: aws.String("1.2.3.4"),
		Tags: []*ec2.Tag{
			{Key: aws.String("rando"), Value: aws.String("rondo")},
		},
	}

	c := mkAwsCollector()

	clusters := map[string]*api.Cluster{
		"c1": {
			Name: "c1",
			Instances: api.Instances{
				{
					Host: "2.3.4.5",
					Port: 8080,
				},
			},
		},
	}
	c.processEC2Instance(clusters, inst)

	assert.DeepEqual(t, clusters, clusters)
}

func TestAWSCollectorGetClusters(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockEC2Svc := newMockEc2Interface(ctrl)

	c := mkAwsCollector()
	c.ec2Svc = mockEC2Svc

	reservations := []*ec2.Reservation{
		{
			Instances: []*ec2.Instance{
				{
					PrivateIpAddress: aws.String("1.2.3.4"),
					Tags: []*ec2.Tag{
						{Key: aws.String("rando"), Value: aws.String("rondo")},
						{Key: aws.String("tbn:cluster:c1:80"), Value: aws.String("")},
						{Key: aws.String("tbn:cluster:c1:80:k1"), Value: aws.String("v11")},
						{Key: aws.String("tbn:cluster:c1:80:k2:wat"), Value: aws.String("v21")},
						{Key: aws.String("tbn:cluster:c2:443:k1"), Value: aws.String("v12")},
						{Key: aws.String("tbn:cluster:c2:443:k2:wat:bro"), Value: aws.String("v22")},
					},
				},
			},
		},
	}

	mockEC2Svc.EXPECT().
		DescribeInstances(&ec2.DescribeInstancesInput{Filters: c.mkFilters()}).
		Return(&ec2.DescribeInstancesOutput{Reservations: reservations}, nil)

	clusters, err := c.getClusters()
	assert.Nil(t, err)
	assert.ArrayEqual(
		t,
		clusters,
		[]api.Cluster{
			{
				Name: "c1",
				Instances: api.Instances{
					{
						Host: "1.2.3.4",
						Port: 80,
						Metadata: api.Metadata{
							{Key: "k1", Value: "v11"},
							{Key: "k2:wat", Value: "v21"},
							{Key: "rando", Value: "rondo"},
						},
					},
				},
			},
			{
				Name: "c2",
				Instances: api.Instances{
					{
						Host: "1.2.3.4",
						Port: 443,
						Metadata: api.Metadata{
							{Key: "k1", Value: "v12"},
							{Key: "k2:wat:bro", Value: "v22"},
							{Key: "rando", Value: "rondo"},
						},
					},
				},
			},
		},
	)
}

func TestAWSCollectorGetClustersError(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockEC2Svc := newMockEc2Interface(ctrl)

	c := mkAwsCollector()
	c.ec2Svc = mockEC2Svc

	mockEC2Svc.EXPECT().
		DescribeInstances(&ec2.DescribeInstancesInput{Filters: c.mkFilters()}).
		Return(nil, errors.New("boom"))

	clusters, err := c.getClusters()
	assert.Nil(t, clusters)
	assert.ErrorContains(t, err, "boom")
}

func TestAWSTagAndPortMap(t *testing.T) {
	tpm := tagAndPortMap{prefix: "tbn:cluster", delimiter: ":"}
	assert.Nil(t, tpm.processTag("foo", "bar"))
	assert.Nil(t, tpm.processTag("taggo", ""))
	assert.Nil(t, tpm.processTag("tbn:cluster:c1:80", ""))
	assert.Nil(t, tpm.processTag("tbn:cluster:c1:80:k1", "v11"))
	assert.Nil(t, tpm.processTag("tbn:cluster:c1:80:k1:wat", "v21"))
	assert.Nil(t, tpm.processTag("tbn:cluster:c2:443", ""))
	assert.Nil(t, tpm.processTag("tbn:cluster:c2:443:k1", "v12"))
	assert.Nil(t, tpm.processTag("tbn:cluster:c2:443:k2", ""))

	wantErrPrefix := `malformed cluster/port in tag key: "c3:some garbage": bad port: strconv.ParseUint: parsing "some garbage": invalid syntax`
	gotErr := tpm.processTag("tbn:cluster:c3:some garbage", "")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	wantErrPrefix = `malformed cluster/port in tag key: "c3:-1": bad port: strconv.ParseUint: parsing "-1": invalid syntax`
	gotErr = tpm.processTag("tbn:cluster:c3:-1", "")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	wantErrPrefix = `malformed cluster/port in tag key: "c3:0": port must be non zero`
	gotErr = tpm.processTag("tbn:cluster:c3:0", "")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	wantErrPrefix = `malformed cluster/port in tag key: "c3:65536": bad port: strconv.ParseUint: parsing "65536": value out of range`
	gotErr = tpm.processTag("tbn:cluster:c3:65536", "")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	assert.Nil(t, tpm.processTag("tbn:cluster:c3:1234:k1", "v13"))

	wantErrPrefix = `tag key empty after "tbn:cluster:" prefix removed: "tbn:cluster:"="nope"`
	gotErr = tpm.processTag("tbn:cluster:", "nope")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	wantErrPrefix = `empty tag key for value: "nope"`
	gotErr = tpm.processTag("", "nope")
	assert.ErrorContains(t, gotErr, wantErrPrefix)

	wantClusterTags := map[clusterAndPort]map[string]string{
		{"c1", 80}: {
			"k1":     "v11",
			"k1:wat": "v21",
		},
		{"c2", 443}: {
			"k1": "v12",
			"k2": "",
		},
		{"c3", 1234}: {
			"k1": "v13",
		},
	}

	wantGlobalTags := map[string]string{"foo": "bar", "taggo": ""}

	assert.DeepEqual(t, tpm.clusterTagMap, wantClusterTags)
	assert.DeepEqual(t, tpm.globalTagMap, wantGlobalTags)
}
