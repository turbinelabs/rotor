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
	"sort"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/arrays/dedupe"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/poller"
)

// requestMatch is a simplified version of a tbnapi.Match that models only
// the request-side of the match (Match.From).
type requestMatch struct {
	metadatum tbnapi.Metadatum
	matchKind tbnapi.MatchKind
}

// processedMatches is a view of all tbnapi.Matches, wherein the request-side
// matches (Match.From) have been separated from the service-side constraints
// (Match.To). A key mapping is preserved to be able to move from the keys
// of the former to the keys of the latter. For both, wildcard values are
// represented with empty strings.
type processedMatches struct {
	matches     []requestMatch
	constraints tbnapi.Metadata
	keyMapping  map[string]string
}

// constraint is a view of a specific cluster constraint
type constraint struct {
	metadata     tbnapi.Metadata
	responseData tbnapi.ResponseData
}

// constaintMap is a simplified ClusterConstraint; it maps a ConstraintKey
// to the constraint metadata.
type constraintMap map[tbnapi.ConstraintKey]constraint

// explodedConstraintMap maps a ConstraintKey to all possible constraint
// metadatas.
type explodedConstraintMap map[tbnapi.ConstraintKey][]constraint

// routeMetadata describes the mapping from requests to cluster subsets. matches
// describes the means by which requests are matched. constraintMeta describes,
// for each constraint key, the metadata to be used to subset the corresponding
// cluster.
type routeMetadata struct {
	matches        []requestMatch
	constraintMeta constraintMap
}

// routeVariant is a single variant of a tbn.Rule that contains fully specified
// metadata values for the matches expressed therein and is used to construct an
// envoy.Route. This includes:
//   - Methods: the HTTP methods (all are matched if empty)
//   - RouteMeta: contains the routeMetadata with with to match headers and the
//     corresponding cluster metadata they map to.
//   - Destination: the set of constrained clusters that requests that match
//     against matches will be routed to
//   - RuleKey: will be non-empty for all routeVariants except the one
//     derived from from the SharedRules.Default, which has no rule
//   - SharedRulesName: will be non-empty for all routeVariants derived from
//     a rule that came from the SharedRules rather than the Route rules.
//   - ResponseData: the response data to be attached to the Route response
type routeVariant struct {
	Methods         []string
	RouteMeta       routeMetadata
	Destination     tbnapi.AllConstraints
	RuleKey         tbnapi.RuleKey
	SharedRulesName string
	ResponseData    tbnapi.ResponseData
}

// routeExploder transforms the rules for a given route into list of
// routeVariants, which can be used to construct envoy.Routes. Wildcard
// matches are fully enumerated according to available cluster subsets.
type routeExploder struct {
	domainConfig  domainConfig
	routeConfig   routeConfig
	clusterForKey func(tbnapi.ClusterKey) tbnapi.Cluster
}

// Explode() produces a slice of routeVariants for a single Route, enumerating
// all combinations of all available values for wildcard matches
func (e routeExploder) Explode() []routeVariant {
	// The order of precedence here follows the semantics of the Turbine Labs API:
	//   - Route.Rules
	//   - Route.SharedRules.Rules
	//   - Route.SharedRules.Default
	// (The rules come pre-aggregated as poller.TaggedRules.)
	var rcs []routeVariant
	for _, rule := range e.routeConfig.AllRules {
		rcs = append(rcs, e.explodeRule(rule)...)
	}

	// Produce a constraintMap for the Default rule.
	defaultConstraints := e.routeConfig.SharedRules.Default.Light

	cmm := make(constraintMap, len(defaultConstraints))
	for _, cc := range defaultConstraints {
		cmm[cc.ConstraintKey] = constraint{cc.Metadata, cc.ResponseData}
	}

	// Merge the route's response data into the shared rules response data.
	mergedResponseData := e.routeConfig.SharedRules.ResponseData.MergeFrom(
		e.routeConfig.Route.ResponseData,
	)

	rcs = append(
		rcs,
		routeVariant{
			RouteMeta:       routeMetadata{constraintMeta: cmm},
			Destination:     e.routeConfig.SharedRules.Default,
			SharedRulesName: e.routeConfig.SharedRules.Name,
			ResponseData:    mergedResponseData,
		},
	)

	return rcs
}

// explodeRule takes a TaggedRule, and produces all available routeVariants.
func (e routeExploder) explodeRule(rule poller.TaggedRule) []routeVariant {
	// fully describe the rule
	pms := processMatches(rule.Rule, e.ruleToStr)
	// find all available constraints
	cmm := explodeConstraints(rule.Constraints.Light, pms, e.findMatchingMetadata)
	// rejigger to combine like-metadata
	rms := coalesceExplodedConstraints(pms, cmm)

	mergedResponseData := e.routeConfig.SharedRules.ResponseData.
		MergeFrom(e.routeConfig.Route.ResponseData)

	// produce the routeVariants
	rcs := make([]routeVariant, 0, len(rms))
	for _, rm := range rms {
		rcs = append(rcs, routeVariant{
			Methods:         rule.Methods,
			RouteMeta:       rm,
			Destination:     rule.Constraints,
			RuleKey:         rule.RuleKey,
			SharedRulesName: rule.SharedRuleName,
			ResponseData:    mergedResponseData,
		})
	}

	return rcs
}

func (e routeExploder) ruleToStr(rule tbnapi.Rule) string {
	return fmt.Sprintf(
		"Domain[%s:%d], Route[%s], Rule[%v]",
		e.domainConfig.Domain.Name,
		e.domainConfig.Domain.Port,
		e.routeConfig.Route.Path,
		rule.RuleKey,
	)
}

// findMatchingMetadata is just a local shim to bind the cluster before calling
// the package findMatchingMetadata function.
func (e routeExploder) findMatchingMetadata(
	constraints tbnapi.Metadata,
	clusterKey tbnapi.ClusterKey,
) []tbnapi.Metadata {
	cluster := e.clusterForKey(clusterKey)
	return findMatchingMetadata(constraints, cluster.Instances)
}

// processMatches produces a struct containing:
//   1. the necessary header matches (wildcard matches have an empty Value)
//   2. the derived cluster constraints (wildcard matches have an empty Value)
//   3. a key mapping from match keys to derived constraint keys
//   4. response data for the rule
func processMatches(
	rule tbnapi.Rule,
	ruleToStr func(tbnapi.Rule) string,
) processedMatches {
	pms := processedMatches{keyMapping: map[string]string{}}

	for _, match := range rule.Matches {
		switch {
		case match.From.Key == "":
			console.Error().Printf("Invalid Match(From.Key nil) for %s", ruleToStr(rule))
			continue

		case match.To.Key == "" && match.To.Value != "":
			console.Error().Printf(
				"Invalid Match(To.Key nil, To.Value non-nil) for %s",
				ruleToStr(rule),
			)
			continue

		case match.To.Key != "":
			// wildcard or specific match, mapped key
			pms.keyMapping[match.From.Key] = match.To.Key

			var value string
			if match.From.Value != "" && match.To.Value == "" {
				// specific match, constraint value is same as match value
				value = match.From.Value
			} else {
				// specific or wildcard match; if specific, single constraint. if
				// wildcard, one constraint per possible value based on cluster instance
				// metadata.
				value = match.To.Value
			}

			pms.constraints = append(
				pms.constraints,
				tbnapi.Metadatum{Key: match.To.Key, Value: value},
			)

		default:
			// no constraint on clusters from the match
		}

		hmm := requestMatch{
			metadatum: match.From,
			matchKind: match.Kind,
		}
		pms.matches = append(pms.matches, hmm)
	}

	return pms
}

// explodeConstraints iterates over ClusterConstraints and explodes them
// according to the processedMatches, producing a map from constraint key to
// every possible slice of cluster metadata constraints.
func explodeConstraints(
	ccs tbnapi.ClusterConstraints,
	pms processedMatches,
	findMatches func(tbnapi.Metadata, tbnapi.ClusterKey) []tbnapi.Metadata,
) explodedConstraintMap {
	ecm := make(explodedConstraintMap, len(ccs))
	for _, cc := range ccs {
		// No keyMapping  means there are no match-derived constraints. In this
		// case, the only cluster constraints are those in cc
		if len(pms.keyMapping) == 0 {
			ecm[cc.ConstraintKey] = []constraint{{cc.Metadata, cc.ResponseData}}
			continue
		}

		// cluster constraints win
		cMap := map[string]string{}
		for _, md := range pms.constraints {
			cMap[md.Key] = md.Value
		}
		for _, md := range cc.Metadata {
			cMap[md.Key] = md.Value
		}

		constraintMetadata := tbnapi.MetadataFromMap(cMap)
		clusterMatches := findMatches(constraintMetadata, cc.ClusterKey)

		constraints := make([]constraint, len(clusterMatches))
		for i, metadata := range clusterMatches {
			constraints[i] = constraint{metadata, cc.ResponseData}
		}

		ecm[cc.ConstraintKey] = constraints
	}
	return ecm
}

// findMatchingMetadata will find all distinct metadata combinations with
// the given instances for which the metadata matches the given constraints.
// constraints with empty values will match all values for that key in the
// instance metadata.
func findMatchingMetadata(
	constraints tbnapi.Metadata,
	instances tbnapi.Instances,
) []tbnapi.Metadata {
	var result []tbnapi.Metadata
	for _, i := range instances {
		mdMap := i.Metadata.Map()
		filledIn := constraints.Map()
		matches := true
		for _, md := range constraints {
			if v, ok := mdMap[md.Key]; ok {
				// matches, already filled in
				if v == md.Value {
					continue
				}
				// matches, fill in from instance metadata
				if md.Value == "" {
					filledIn[md.Key] = v
					continue
				}
			}
			// didn't match, don't bother with the rest
			matches = false
			break
		}
		if matches {
			result = append(result, tbnapi.MetadataFromMap(filledIn))
		}
	}

	return dedupeMetadata(result)
}

type dedupableMetadata struct {
	m []tbnapi.Metadata
}

func (m *dedupableMetadata) Len() int            { return len(m.m) }
func (m *dedupableMetadata) Equal(i, j int) bool { return m.m[i].Equals(m.m[j]) }
func (m *dedupableMetadata) Remove(i int)        { m.m = append(m.m[0:i], m.m[i+1:]...) }
func (m *dedupableMetadata) Swap(i, j int)       { m.m[i], m.m[j] = m.m[j], m.m[i] }
func (m *dedupableMetadata) Less(i, j int) bool {
	return m.m[i].Compare(m.m[j]) < 0
}

// dedupMetadata removes duplicates from a []tbnapi.Metadata
func dedupeMetadata(md []tbnapi.Metadata) []tbnapi.Metadata {
	m := &dedupableMetadata{md}
	sort.Sort(m)
	dedupe.Dedupe(m)
	return m.m
}

// coalesceExplodedConstraints takes the processedMatches and
// explodedConstraintMap, and produces a slice of routeMetadata, where each
// entry corresponds to one specific variant of a wildcard match. A BST is used
// to consolidate cluster constraints with the same match metadata.
func coalesceExplodedConstraints(
	pms processedMatches,
	ecm explodedConstraintMap,
) []routeMetadata {
	var smt satisfiedMatchesTree

	for cKey, constraints := range ecm {
		for _, constraint := range constraints {
			metaMap := constraint.metadata.Map()
			filledInConstraints := make(tbnapi.Metadata, 0, len(pms.constraints))
			for _, c := range pms.constraints {
				filledInConstraints = append(
					filledInConstraints,
					tbnapi.Metadatum{Key: c.Key, Value: metaMap[c.Key]},
				)
			}
			smt.insert(filledInConstraints, cKey, constraint)
		}
	}

	allSatisfiedMatches := smt.slice()
	var routeMetas []routeMetadata

	for _, i := range allSatisfiedMatches {
		cMap := i.constraints.Map()
		filledInMatches := make([]requestMatch, 0, len(pms.matches))
		for _, hmm := range pms.matches {
			md := hmm.metadatum
			if md.Value == "" {
				md.Value = cMap[pms.keyMapping[md.Key]]
			}
			filledInMatches = append(
				filledInMatches,
				requestMatch{metadatum: md, matchKind: hmm.matchKind},
			)
		}
		routeMetas = append(
			routeMetas,
			routeMetadata{matches: filledInMatches, constraintMeta: i.constraintMeta},
		)
	}

	return routeMetas
}

// satisfiedMatches combines service-side constraints (some of which may
// be wildcards, represented by empty values), and a map of clusters and
// their corresponding fully-specified constraints.
type satisfiedMatches struct {
	constraints    tbnapi.Metadata
	constraintMeta constraintMap
}

// a satisfiedMatchesTree is effectively a
// map[tbnapi.Metadata]map[tbnapi.ConstraintKey]constraint, but uses a
// binary search tree to accomplish inserts (since Metadata cannot be map keys)
type satisfiedMatchesTree struct {
	satisfiedMatches *satisfiedMatches
	left             *satisfiedMatchesTree
	right            *satisfiedMatchesTree
}

// this is the real trick; it needs to find the relevant satisfiedMatches,
// and add the cluster key and metadata to it.
func (t *satisfiedMatchesTree) insert(
	constraints tbnapi.Metadata,
	cKey tbnapi.ConstraintKey,
	constraintMeta constraint,
) {
	if t.satisfiedMatches == nil {
		t.satisfiedMatches = &satisfiedMatches{
			constraints,
			constraintMap{cKey: constraintMeta},
		}
	}

	cmp := t.satisfiedMatches.constraints.Compare(constraints)
	switch {
	case cmp > 0:
		// this node greater than new value, insert on left
		if t.left == nil {
			t.left = &satisfiedMatchesTree{}
		}
		t.left.insert(constraints, cKey, constraintMeta)

	case cmp < 0:
		// this node less than new value, insert on right
		if t.right == nil {
			t.right = &satisfiedMatchesTree{}
		}
		t.right.insert(constraints, cKey, constraintMeta)

	default:
		t.satisfiedMatches.constraintMeta[cKey] = constraintMeta
	}
}

func (t *satisfiedMatchesTree) slice() []satisfiedMatches {
	var result []satisfiedMatches
	if t.left != nil {
		result = append(result, t.left.slice()...)
	}
	if t.satisfiedMatches != nil {
		result = append(result, *t.satisfiedMatches)
	}
	if t.right != nil {
		result = append(result, t.right.slice()...)
	}
	return result
}
