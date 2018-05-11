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

package poller

import (
	"testing"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

func TestObjectTerribleHash(t *testing.T) {
	assert.Equal(t, MkFixtureObjects().TerribleHash(), FixtureHash)
}

func TestObjectsCheckIntegrityOK(t *testing.T) {
	assert.Nil(t, MkFixtureObjects().CheckIntegrity())
}

func TestObjectsCheckIntegrityMissingCluster(t *testing.T) {
	objects := MkFixtureObjects()
	objects.Clusters = objects.Clusters[1:]
	assert.ErrorContains(t, objects.CheckIntegrity(), "unknown cluster C0")
}

func TestObjectsCheckIntegrityMissingSharedRules(t *testing.T) {
	objects := MkFixtureObjects()
	objects.SharedRules = objects.SharedRules[1:]
	assert.ErrorContains(t, objects.CheckIntegrity(), "unknown shared_rules SRK-0")
}

func TestObjectsCheckIntegrityMissingDomain(t *testing.T) {
	objects := MkFixtureObjects()
	objects.Domains = objects.Domains[1:]
	assert.ErrorContains(t, objects.CheckIntegrity(), "unknown domain D0")
}

func TestObjectsClusterFromKey(t *testing.T) {
	objects := MkFixtureObjects()
	cluster := objects.ClusterFromKey(api.ClusterKey("C0"))
	assert.True(t, cluster.Equals(objects.Clusters[0]))
}

func TestObjectsClusterFromKeyMissing(t *testing.T) {
	objects := MkFixtureObjects()
	cluster := objects.ClusterFromKey(api.ClusterKey("nope"))
	assert.True(t, cluster.Equals(api.Cluster{}))
}

func TestObjectsSharedRulesFromKey(t *testing.T) {
	objects := MkFixtureObjects()
	sr := objects.SharedRulesFromKey(api.SharedRulesKey("SRK-2"))
	assert.True(t, sr.Equals(objects.SharedRules[2]))
}

func TestObjectsSharedRulesFromKeyMissing(t *testing.T) {
	objects := MkFixtureObjects()
	cluster := objects.SharedRulesFromKey(api.SharedRulesKey("nope"))
	assert.True(t, cluster.Equals(api.SharedRules{}))
}

func TestObjectsDomainsPerPort(t *testing.T) {
	objects := MkFixtureObjects()
	domains := objects.DomainsPerPort(8080)
	assert.DeepEqual(t, domains, []api.Domain{objects.Domains[1]})
}

func TestObjectsDomainsPerPortsMissing(t *testing.T) {
	assert.Nil(t, MkFixtureObjects().DomainsPerPort(1234))
}

func TestObjectsRoutesPerDomain(t *testing.T) {
	objects := MkFixtureObjects()
	routes := objects.RoutesPerDomain(api.DomainKey("D0"))
	assert.Equal(t, len(routes), 2)
	assert.Equal(t, routes[0].RouteKey, api.RouteKey("R0"))
	assert.Equal(t, routes[1].RouteKey, api.RouteKey("R1"))
}

func TestObjectsRoutesPerDomainMissing(t *testing.T) {
	assert.Nil(t, MkFixtureObjects().RoutesPerDomain(api.DomainKey("nope")))
}

func TestObjectsAllPorts(t *testing.T) {
	assert.HasSameElements(t, MkFixtureObjects().AllPorts(), []int{8080, 8443})
}

func TestObjectsSubsetsPerCluster(t *testing.T) {
	objects := MkFixtureObjects()
	want := [][]string{
		{"build", "stage", "sw_version"},
		{"stage"},
		{"stage", "sw_version"},
	}
	assert.DeepEqual(t, objects.SubsetsPerCluster(api.ClusterKey("C0")), want)
}

func TestObjectsSubsetsPerClusterEmpty(t *testing.T) {
	assert.Nil(t, MkFixtureObjects().SubsetsPerCluster(api.ClusterKey("C2")))
}

func TestObjectsSubsetsPerClusterMissing(t *testing.T) {
	assert.Nil(t, MkFixtureObjects().SubsetsPerCluster(api.ClusterKey("nope")))
}

func TestObjectsAggregateRules(t *testing.T) {
	objects := MkFixtureObjects()
	rules := objects.AggregateRules(objects.Routes[2])
	assert.Equal(t, len(rules), 2)
	assert.Equal(t, rules[0].RuleKey, api.RuleKey("R2R0"))
	assert.Equal(t, rules[0].SharedRuleName, "")
	assert.Equal(t, rules[1].RuleKey, api.RuleKey("SRK2-R2R0"))
	assert.Equal(t, rules[1].SharedRuleName, "SRK-2")
}

func TestObjectsAggregateRulesEmpty(t *testing.T) {
	objects := MkFixtureObjects()
	rules := objects.AggregateRules(objects.Routes[0])
	assert.Nil(t, rules)
}
