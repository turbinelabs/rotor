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

package kubernetes

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type clientFromFlags -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

import (
	k8s "k8s.io/client-go/kubernetes"
	k8sclient "k8s.io/client-go/rest"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
)

// clientFromFlags represents the command-line flags specifying configuration of an
// unversioned kubernetes client.
type clientFromFlags interface {
	// Make produces an unversioned kubernetes client from the provided flags,
	// or an error
	Make() (*k8s.Clientset, error)
}

// NewFromFlags produces a clientFromFlags, adding necessary flags to the provided
// flag.FlagSet
func newClientFromFlags(flagset tbnflag.FlagSet) clientFromFlags {
	ff := &clientFromFlagsImpl{}

	const hostNote = "Only used if -kubernetes-host is set."

	flagset.StringVar(
		&ff.caCertFile,
		"ca-cert",
		"",
		"The `path` to a trusted root certificate file for the Kubernetes API server. "+hostNote)

	flagset.StringVar(
		&ff.clientKeyFile,
		"client-key",
		"",
		"The `path` to a certificate key file which the client will use to authenticate itself to the Kubernetes API server. "+hostNote)

	flagset.StringVar(
		&ff.clientCertFile,
		"client-cert",
		"",
		"The `path` to a certificate file which the client will use to authenticate itself to the Kubernetes API server. "+hostNote)

	flagset.StringVar(
		&ff.k8sApiHost,
		"kubernetes-host",
		"",
		"The `host` name for the kubernetes API server. Required if the collector is to run outside of the Kubernetes cluster.")

	return ff
}

type clientFromFlagsImpl struct {
	k8sApiHost     string
	caCertFile     string
	clientKeyFile  string
	clientCertFile string
}

func (ff *clientFromFlagsImpl) configureExternalClient() (*k8s.Clientset, error) {
	config := &k8sclient.Config{
		Host: ff.k8sApiHost,
		TLSClientConfig: k8sclient.TLSClientConfig{
			CertFile: ff.clientCertFile,
			KeyFile:  ff.clientKeyFile,
			CAFile:   ff.caCertFile,
		},
	}

	return k8s.NewForConfig(config)
}

func (ff *clientFromFlagsImpl) configureInternalClient() (*k8s.Clientset, error) {
	config, err := k8sclient.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return k8s.NewForConfig(config)
}

func (ff *clientFromFlagsImpl) Make() (*k8s.Clientset, error) {
	if ff.k8sApiHost != "" {
		return ff.configureExternalClient()
	} else {
		return ff.configureInternalClient()
	}
}
