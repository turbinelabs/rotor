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
	"fmt"
	"testing"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/test/assert"
)

func testRuleToStr(rule tbnapi.Rule) string {
	return fmt.Sprintf("Rule[%v]", rule.RuleKey)
}

func TestProcessMatchesNoMatches(t *testing.T) {
	want := processedMatches{keyMapping: map[string]string{}}
	got := processMatches(tbnapi.Rule{}, testRuleToStr)
	assert.DeepEqual(t, got, want)
}

func TestProcessMatches(t *testing.T) {
	rule := tbnapi.Rule{
		Matches: tbnapi.Matches{
			{
				// From.Key == ""
			},
			{
				From: tbnapi.Metadatum{Key: "to.value with empty to.key"},
				To:   tbnapi.Metadatum{Value: "non-empty"},
			},
			{
				From: tbnapi.Metadatum{
					Key:   "specific match",
					Value: "specific value",
				},
				To:   tbnapi.Metadatum{Key: "specific mapped key"},
				Kind: tbnapi.HeaderMatchKind,
			},
			{
				From: tbnapi.Metadatum{Key: "wildcard match"},
				To:   tbnapi.Metadatum{Key: "wildcard mapped key"},
				Kind: tbnapi.CookieMatchKind,
			},
			{
				From: tbnapi.Metadatum{
					Key:   "another specific match",
					Value: "another specific value",
				},
				To: tbnapi.Metadatum{
					Key:   "another specific mapped key",
					Value: "specific mapped value",
				},
				Kind: tbnapi.HeaderMatchKind,
			},
			{
				From: tbnapi.Metadatum{
					Key:   "specific match (no constraint)",
					Value: "specific value (no constraint)",
				},
				Kind: tbnapi.CookieMatchKind,
			},
			{
				From: tbnapi.Metadatum{Key: "wildcard match (no constraint)"},
				Kind: tbnapi.HeaderMatchKind,
			},
		},
	}

	ruleToStrCalls := 0
	ruleToStr := func(got tbnapi.Rule) string {
		assert.DeepEqual(t, got, rule)
		ruleToStrCalls++
		return "ah!"
	}

	want := processedMatches{
		matches: []requestMatch{
			{
				matchKind: tbnapi.HeaderMatchKind,
				metadatum: tbnapi.Metadatum{
					Key:   "specific match",
					Value: "specific value",
				},
			},
			{
				matchKind: tbnapi.CookieMatchKind,
				metadatum: tbnapi.Metadatum{Key: "wildcard match"},
			},
			{
				matchKind: tbnapi.HeaderMatchKind,
				metadatum: tbnapi.Metadatum{
					Key:   "another specific match",
					Value: "another specific value",
				},
			},
			{
				matchKind: tbnapi.CookieMatchKind,
				metadatum: tbnapi.Metadatum{
					Key:   "specific match (no constraint)",
					Value: "specific value (no constraint)",
				},
			},
			{
				matchKind: tbnapi.HeaderMatchKind,
				metadatum: tbnapi.Metadatum{Key: "wildcard match (no constraint)"},
			},
		},
		constraints: tbnapi.Metadata{
			{
				Key:   "specific mapped key",
				Value: "specific value",
			},
			{Key: "wildcard mapped key"},
			{
				Key:   "another specific mapped key",
				Value: "specific mapped value",
			},
		},
		keyMapping: map[string]string{
			"specific match":         "specific mapped key",
			"wildcard match":         "wildcard mapped key",
			"another specific match": "another specific mapped key",
		},
	}
	got := processMatches(rule, ruleToStr)
	assert.DeepEqual(t, got, want)
	assert.Equal(t, ruleToStrCalls, 2)
}

func TestExplodeConstraints(t *testing.T) {
	ccs := tbnapi.ClusterConstraints{
		{
			ConstraintKey: "cc1",
			ClusterKey:    "cl1",
			Metadata: tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1a"},
				{Key: "cl1k1", Value: "ck1v1"},
				{Key: "cl1k2", Value: "ck1v2"},
			},
		},
		{
			ConstraintKey: "cc2",
			ClusterKey:    "cl2",
			Metadata: tbnapi.Metadata{
				{Key: "cl2k1", Value: "ck2v1"},
				{Key: "cl2k2", Value: "ck2v2"},
			},
		},
		{
			ConstraintKey: "cc3",
			ClusterKey:    "cl3",
			Metadata: tbnapi.Metadata{
				{Key: "cl3k1", Value: "ck3v1"},
				{Key: "cl3k2", Value: "ck3v2"},
			},
		},
	}

	pms := processedMatches{
		// matches unused
		constraints: tbnapi.Metadata{
			{Key: "c1k1", Value: "c1v1"},
			{Key: "c2k1", Value: "c2v1"},
			{Key: "c3k1", Value: "c3v1"},
		},
		// keyMapping unused apart from length test
		keyMapping: map[string]string{"some": "stuff"},
	}

	findMatches := func(
		constraints tbnapi.Metadata,
		clusterKey tbnapi.ClusterKey,
	) []tbnapi.Metadata {
		switch clusterKey {
		case "cl1":
			want := tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1a"},
				{Key: "c2k1", Value: "c2v1"},
				{Key: "c3k1", Value: "c3v1"},
				{Key: "cl1k1", Value: "ck1v1"},
				{Key: "cl1k2", Value: "ck1v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl1md"}}}

		case "cl2":
			want := tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1"},
				{Key: "c2k1", Value: "c2v1"},
				{Key: "c3k1", Value: "c3v1"},
				{Key: "cl2k1", Value: "ck2v1"},
				{Key: "cl2k2", Value: "ck2v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl2md"}}}

		case "cl3":
			want := tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1"},
				{Key: "c2k1", Value: "c2v1"},
				{Key: "c3k1", Value: "c3v1"},
				{Key: "cl3k1", Value: "ck3v1"},
				{Key: "cl3k2", Value: "ck3v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl3md"}}}

		default:
			assert.Failed(t, "unexpected cluster key: "+string(clusterKey))
			return nil
		}
	}

	want := explodedConstraintMap{
		"cc1": []constraint{{metadata: tbnapi.Metadata{{Key: "cl1md"}}}},
		"cc2": []constraint{{metadata: tbnapi.Metadata{{Key: "cl2md"}}}},
		"cc3": []constraint{{metadata: tbnapi.Metadata{{Key: "cl3md"}}}},
	}

	got := explodeConstraints(ccs, pms, findMatches)
	assert.DeepEqual(t, got, want)
}

func TestExplodeConstraintsNoKeyMapping(t *testing.T) {
	ccs := tbnapi.ClusterConstraints{
		{
			ConstraintKey: "cc1",
			ClusterKey:    "cl1",
			Metadata: tbnapi.Metadata{
				{Key: "cl1k1", Value: "ck1v1"},
				{Key: "cl1k2", Value: "ck1v2"},
			},
		},
		{
			ConstraintKey: "cc2",
			ClusterKey:    "cl2",
			Metadata: tbnapi.Metadata{
				{Key: "cl2k1", Value: "ck2v1"},
				{Key: "cl2k2", Value: "ck2v2"},
			},
		},
		{
			ConstraintKey: "cc3",
			ClusterKey:    "cl3",
			Metadata: tbnapi.Metadata{
				{Key: "cl3k1", Value: "ck3v1"},
				{Key: "cl3k2", Value: "ck3v2"},
			},
		},
	}

	want := explodedConstraintMap{
		"cc1": []constraint{{metadata: ccs[0].Metadata}},
		"cc2": []constraint{{metadata: ccs[1].Metadata}},
		"cc3": []constraint{{metadata: ccs[2].Metadata}},
	}

	got := explodeConstraints(ccs, processedMatches{}, nil)
	assert.DeepEqual(t, got, want)
}

func TestExplodeConstraintsNoClusterConstraints(t *testing.T) {
	want := explodedConstraintMap{}
	got := explodeConstraints(nil, processedMatches{}, nil)
	assert.DeepEqual(t, got, want)
}

func TestExplodeConstraintsNoConstraints(t *testing.T) {
	ccs := tbnapi.ClusterConstraints{
		{
			ConstraintKey: "cc1",
			ClusterKey:    "cl1",
			Metadata: tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1a"},
				{Key: "cl1k1", Value: "ck1v1"},
				{Key: "cl1k2", Value: "ck1v2"},
			},
		},
		{
			ConstraintKey: "cc2",
			ClusterKey:    "cl2",
			Metadata: tbnapi.Metadata{
				{Key: "cl2k1", Value: "ck2v1"},
				{Key: "cl2k2", Value: "ck2v2"},
			},
		},
		{
			ConstraintKey: "cc3",
			ClusterKey:    "cl3",
			Metadata: tbnapi.Metadata{
				{Key: "cl3k1", Value: "ck3v1"},
				{Key: "cl3k2", Value: "ck3v2"},
			},
		},
	}

	pms := processedMatches{
		// keyMapping unused apart from length test
		keyMapping: map[string]string{"some": "stuff"},
	}

	findMatches := func(
		constraints tbnapi.Metadata,
		clusterKey tbnapi.ClusterKey,
	) []tbnapi.Metadata {
		switch clusterKey {
		case "cl1":
			want := tbnapi.Metadata{
				{Key: "c1k1", Value: "c1v1a"},
				{Key: "cl1k1", Value: "ck1v1"},
				{Key: "cl1k2", Value: "ck1v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl1md"}}}

		case "cl2":
			want := tbnapi.Metadata{
				{Key: "cl2k1", Value: "ck2v1"},
				{Key: "cl2k2", Value: "ck2v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl2md"}}}

		case "cl3":
			want := tbnapi.Metadata{
				{Key: "cl3k1", Value: "ck3v1"},
				{Key: "cl3k2", Value: "ck3v2"},
			}
			assert.True(t, constraints.Equals(want))
			return []tbnapi.Metadata{{tbnapi.Metadatum{Key: "cl3md"}}}

		default:
			assert.Failed(t, "unexpected cluster key: "+string(clusterKey))
			return nil
		}
	}

	want := explodedConstraintMap{
		"cc1": []constraint{{metadata: tbnapi.Metadata{{Key: "cl1md"}}}},
		"cc2": []constraint{{metadata: tbnapi.Metadata{{Key: "cl2md"}}}},
		"cc3": []constraint{{metadata: tbnapi.Metadata{{Key: "cl3md"}}}},
	}

	got := explodeConstraints(ccs, pms, findMatches)
	assert.DeepEqual(t, got, want)
}

func TestCoalesceExplodedConstraints(t *testing.T) {
	pms := processedMatches{
		// matches unused
		matches: []requestMatch{
			{metadatum: tbnapi.Metadatum{Key: "m1k1", Value: "m1v1"}},
			{metadatum: tbnapi.Metadatum{Key: "m2k1", Value: ""}},
			{metadatum: tbnapi.Metadatum{Key: "m3k1", Value: ""}},
		},
		constraints: tbnapi.Metadata{
			{Key: "c1k1"},
			{Key: "c2k1"},
			{Key: "c3k1"},
		},
		// keyMapping unused apart from length test
		keyMapping: map[string]string{
			"m2k1": "c2k1",
			"m3k1": "c3k1",
		},
	}

	ecm := explodedConstraintMap{
		"ck1": []constraint{
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v1"},
					{Key: "c2k1", Value: "c2v1"},
					{Key: "c3k1", Value: "c3v1"},
					{Key: "some", Value: "other"},
				},
			},
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v2"},
					{Key: "c2k1", Value: "c2v2"},
					{Key: "c3k1", Value: "c3v2"},
				},
			},
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v3"},
					{Key: "c2k1", Value: "c2v3"},
					{Key: "c3k1", Value: "c3v3"},
				},
			},
		},
		"ck2": []constraint{
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v1"},
					{Key: "c2k1", Value: "c2v1"},
					{Key: "c3k1", Value: "c3v1"},
				},
			},
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v2"},
					{Key: "c2k1", Value: "c2v2"},
					{Key: "c3k1", Value: "c3v2"},
				},
			},
		},
		"ck3": []constraint{
			{
				metadata: tbnapi.Metadata{
					{Key: "c1k1", Value: "c1v1"},
					{Key: "c2k1", Value: "c2v1"},
					{Key: "c3k1", Value: "c3v1"},
				},
			},
		},
	}

	want := []routeMetadata{
		{
			matches: []requestMatch{
				{metadatum: tbnapi.Metadatum{Key: "m1k1", Value: "m1v1"}},
				{metadatum: tbnapi.Metadatum{Key: "m2k1", Value: "c2v1"}},
				{metadatum: tbnapi.Metadatum{Key: "m3k1", Value: "c3v1"}},
			},
			constraintMeta: constraintMap{
				"ck1": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v1"},
						{Key: "c2k1", Value: "c2v1"},
						{Key: "c3k1", Value: "c3v1"},
						{Key: "some", Value: "other"},
					},
				},
				"ck2": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v1"},
						{Key: "c2k1", Value: "c2v1"},
						{Key: "c3k1", Value: "c3v1"},
					},
				},
				"ck3": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v1"},
						{Key: "c2k1", Value: "c2v1"},
						{Key: "c3k1", Value: "c3v1"},
					},
				},
			},
		},
		{
			matches: []requestMatch{
				{metadatum: tbnapi.Metadatum{Key: "m1k1", Value: "m1v1"}},
				{metadatum: tbnapi.Metadatum{Key: "m2k1", Value: "c2v2"}},
				{metadatum: tbnapi.Metadatum{Key: "m3k1", Value: "c3v2"}},
			},
			constraintMeta: constraintMap{
				"ck1": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v2"},
						{Key: "c2k1", Value: "c2v2"},
						{Key: "c3k1", Value: "c3v2"},
					},
				},
				"ck2": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v2"},
						{Key: "c2k1", Value: "c2v2"},
						{Key: "c3k1", Value: "c3v2"},
					},
				},
			},
		},
		{
			matches: []requestMatch{
				{metadatum: tbnapi.Metadatum{Key: "m1k1", Value: "m1v1"}},
				{metadatum: tbnapi.Metadatum{Key: "m2k1", Value: "c2v3"}},
				{metadatum: tbnapi.Metadatum{Key: "m3k1", Value: "c3v3"}},
			},
			constraintMeta: constraintMap{
				"ck1": {
					metadata: tbnapi.Metadata{
						{Key: "c1k1", Value: "c1v3"},
						{Key: "c2k1", Value: "c2v3"},
						{Key: "c3k1", Value: "c3v3"},
					},
				},
			},
		},
	}

	got := coalesceExplodedConstraints(pms, ecm)
	assert.DeepEqual(t, want, got)
}

func TestFindMatchMetadataNoInstances(t *testing.T) {
	constraints := tbnapi.Metadata{
		{Key: "Key1", Value: "Value1"},
		{Key: "Key2", Value: "Value2"},
		{Key: "Key3", Value: ""},
		{Key: "Key4", Value: ""},
	}

	var instances tbnapi.Instances
	var want []tbnapi.Metadata

	got := findMatchingMetadata(constraints, instances)
	assert.ArrayEqual(t, got, want)
}

func TestFindMatchMetadataNoConstraints(t *testing.T) {
	var constraints tbnapi.Metadata
	instances := tbnapi.Instances{
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
				{Key: "Key3", Value: "Value3a"},
				{Key: "Key4", Value: "Value4a"},
			},
		},
	}
	// one, empty metadata
	want := []tbnapi.Metadata{{}}

	got := findMatchingMetadata(constraints, instances)
	assert.ArrayEqual(t, got, want)
}

func TestFindMatchingMetadata(t *testing.T) {
	constraints := tbnapi.Metadata{
		{Key: "Key1", Value: "Value1"},
		{Key: "Key2", Value: "Value2"},
		{Key: "Key3", Value: ""},
		{Key: "Key4", Value: ""},
	}

	instances := tbnapi.Instances{
		// empty
		{Metadata: tbnapi.Metadata{}},
		// ok
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
				{Key: "Key3", Value: "Value3a"},
				{Key: "Key4", Value: "Value4a"},
			},
		},
		// non-matching fixed value
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Doesn't Match"},
				{Key: "Key3", Value: "Value3b"},
				{Key: "Key4", Value: "Value4b"},
			},
		},
		// ok, differently ordered, already seen
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key3", Value: "Value3a"},
				{Key: "Key4", Value: "Value4a"},
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
			},
		},
		// ok
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
				{Key: "Key4", Value: "Value4c"},
				{Key: "Key3", Value: "Value3c"},
			},
		},
		// missing a fixed value
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key3", Value: "Value3a"},
				{Key: "Key4", Value: "Value4c"},
			},
		},
		// missing a dynamic value
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
				{Key: "Key4", Value: "Value4d"},
			},
		},
		// ok, differently ordered
		{
			Metadata: tbnapi.Metadata{
				{Key: "Key4", Value: "Value4e"},
				{Key: "Key5", Value: "Value5e"},
				{Key: "Key6", Value: "Value6e"},
				{Key: "Key1", Value: "Value1"},
				{Key: "Key2", Value: "Value2"},
				{Key: "Key3", Value: "Value3e"},
			},
		},
	}

	want := []tbnapi.Metadata{
		{
			{Key: "Key1", Value: "Value1"},
			{Key: "Key2", Value: "Value2"},
			{Key: "Key3", Value: "Value3a"},
			{Key: "Key4", Value: "Value4a"},
		},
		{
			{Key: "Key1", Value: "Value1"},
			{Key: "Key2", Value: "Value2"},
			{Key: "Key3", Value: "Value3c"},
			{Key: "Key4", Value: "Value4c"},
		},
		{
			{Key: "Key1", Value: "Value1"},
			{Key: "Key2", Value: "Value2"},
			{Key: "Key3", Value: "Value3e"},
			{Key: "Key4", Value: "Value4e"},
		},
	}

	got := findMatchingMetadata(constraints, instances)
	assert.ArrayEqual(t, got, want)
}

func TestSatisfiedMatchesTreeSliceNoInsert(t *testing.T) {
	var want []satisfiedMatches
	assert.DeepEqual(t, (&satisfiedMatchesTree{}).slice(), want)
}

func TestSatisfiedMatchesTreeSlice(t *testing.T) {
	ck1 := tbnapi.ConstraintKey("CK1")
	ck2 := tbnapi.ConstraintKey("CK2")

	md1 := tbnapi.Metadata{
		{Key: "c", Value: "d"},
		{Key: "a", Value: "b"},
	}

	md2 := tbnapi.Metadata{
		{Key: "a", Value: "b"},
		{Key: "c", Value: "d"},
		{Key: "e", Value: "f"},
	}

	md3 := tbnapi.Metadata{
		{Key: "a", Value: "b"},
		{Key: "c", Value: "d"},
		{Key: "e", Value: "g"},
	}

	md4 := tbnapi.Metadata{
		{Key: "a", Value: "b"},
		{Key: "c", Value: "d"},
		{Key: "e", Value: "f"},
		{Key: "x", Value: "x"},
	}

	md5 := tbnapi.Metadata{
		{Key: "a", Value: "b"},
		{Key: "c", Value: "d"},
		{Key: "e", Value: "f"},
		{Key: "y", Value: "y"},
	}

	cm1 := constraint{
		metadata: tbnapi.Metadata{
			{Key: "CM1K", Value: "CM1V"},
		},
	}

	cm2 := constraint{
		metadata: tbnapi.Metadata{
			{Key: "CM2K", Value: "CM2V"},
		},
		responseData: tbnapi.ResponseData{
			Headers: []tbnapi.HeaderDatum{{tbnapi.ResponseDatum{Name: "bob", Value: "newhart"}}},
		},
	}

	smt := &satisfiedMatchesTree{}

	smt.insert(md1, ck1, cm1)
	smt.insert(md4, ck1, cm1)
	smt.insert(md3, ck1, cm1)
	smt.insert(md5, ck1, cm1)
	smt.insert(md1, ck2, cm2)
	smt.insert(md2, ck1, cm1)
	smt.insert(tbnapi.Metadata{}, ck1, cm1)

	want := []satisfiedMatches{
		{
			constraints: tbnapi.Metadata{},
			constraintMeta: constraintMap{
				ck1: cm1,
			},
		},
		{
			constraints: md1,
			constraintMeta: constraintMap{
				ck1: cm1,
				ck2: cm2,
			},
		},
		{
			constraints: md2,
			constraintMeta: constraintMap{
				ck1: cm1,
			},
		},
		{
			constraints: md4,
			constraintMeta: constraintMap{
				ck1: cm1,
			},
		},
		{
			constraints: md5,
			constraintMeta: constraintMap{
				ck1: cm1,
			},
		},
		{
			constraints: md3,
			constraintMeta: constraintMap{
				ck1: cm1,
			},
		},
	}

	got := smt.slice()
	assert.DeepEqual(t, got, want)
}
