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

package marathon

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type clientFromFlags -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	"crypto/tls"
	"net/http"

	marathon "github.com/gambol99/go-marathon"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor/plugins/marathon/config"
)

// clientFromFlags represents the command-line flags specifying configuration of a Marathon client.
type clientFromFlags interface {
	Validate() error
	Make() (marathon.Marathon, error)
}

// newClientFromFlags produces a clientFromFlags, adding necessary flags to the provided flag.FlagSet
func newClientFromFlags(fs tbnflag.FlagSet) clientFromFlags {
	return &clientFromFlagsImpl{config.NewFromFlags(fs)}
}

type clientFromFlagsImpl struct {
	configFromFlags config.FromFlags
}

func (ff *clientFromFlagsImpl) Validate() error {
	return ff.configFromFlags.Validate()
}

func (ff *clientFromFlagsImpl) Make() (marathon.Marathon, error) {
	ccfg, err := ff.configFromFlags.Make()
	if err != nil {
		return nil, err
	}

	mcfg := marathon.NewDefaultConfig()
	mcfg.URL = ccfg.URL
	mcfg.DCOSToken = ccfg.ACSToken
	mcfg.HTTPClient = &http.Client{
		Timeout: ccfg.RequestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: ccfg.Insecure},
		},
	}
	return marathon.NewClient(mcfg)
}
