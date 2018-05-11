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

// Package collector includes common types for the v1 and v2 CDS/EDS collectors
package collector

import (
	"io"
	"math/rand"

	"github.com/turbinelabs/api"
)

//go:generate mockgen -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

// ClusterResolver takes the name of an envoy cluster and returns the
// tbn.Instances that belong to it. Errors are collected as they are encountered
// and returned along with results to allow a best effort resolution of
// instances (i.e. non-empty errors does not imply that api.Instances is empty).
type ClusterResolver = func(clusterName string) (api.Instances, []error)

// RandomInstanceSelector picks out a random Instances from provided slice.
func RandomInstanceSelector(is api.Instances) api.Instance {
	return is[rand.Intn(len(is))]
}

// ClusterCollector collects tbn.Clusters from a CDS server. Errors are
// collected as they are encountered and returned, in a map keyed by cluster
// name, along with results to allow a best effort resolution of clusters and
// instances (i.e. non-empty errors does not imply that api.Clusters is empty).
type ClusterCollector interface {
	io.Closer
	Collect() (api.Clusters, map[string][]error)
}
