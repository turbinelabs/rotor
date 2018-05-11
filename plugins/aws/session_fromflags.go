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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/flag/usage"
)

// sessionFromFlags represents the command-line flags specifying configuration of
// an AWS client session.
type sessionFromFlags interface {
	// Make produces an AWS client session
	Make() *session.Session
}

// NewFromFlags produces a FromFlags, adding necessary flags to the provided
// flag.FlagSet
func newSessionFromFlags(fs tbnflag.FlagSet) sessionFromFlags {
	ff := &sessionFromFlagsImpl{}

	fs.StringVar(
		&ff.awsRegion,
		"aws.region",
		"",
		usage.Required("The AWS region in which the binary is running"),
	)

	fs.StringVar(
		&ff.awsSecretAccessKey,
		"aws.secret-access-key",
		"",
		usage.Required("The AWS API secret access key"),
	)

	fs.StringVar(
		&ff.awsAccessKeyID,
		"aws.access-key-id",
		"",
		usage.Required("The AWS API access key ID"),
	)

	return ff
}

type sessionFromFlagsImpl struct {
	awsRegion          string
	awsSecretAccessKey string
	awsAccessKeyID     string
}

func (ff *sessionFromFlagsImpl) Make() *session.Session {
	return session.New(&aws.Config{
		Region: aws.String(ff.awsRegion),
		Credentials: credentials.NewStaticCredentials(
			ff.awsAccessKeyID,
			ff.awsSecretAccessKey,
			"",
		),
	})
}
