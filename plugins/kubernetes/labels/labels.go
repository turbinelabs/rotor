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

// Package labels is a shameless veneer on top of k8s.io/apimachinery/pkg/labels
// to hide the dependency so it can be safely vendored. This is the
// minimum necessary implementation to support the kubernetes collector.
package labels

import k8slabels "k8s.io/apimachinery/pkg/labels"

// Labels allows you to present labels independently from their storage.
type Labels interface {
	// Has returns whether the provided label exists.
	Has(label string) (exists bool)

	// Get returns the value for the provided label.
	Get(label string) (value string)
}

// Selector represents a label selector. This is a subset of
// k8s.io/apimachinery/pkg/labels/Selector, added here for code portability
type Selector interface {
	// Matches returns true if this selector matches the given set of labels.
	Matches(Labels) bool
}

type selector struct {
	underlying k8slabels.Selector
}

func (s selector) Matches(labels Labels) bool {
	return s.underlying.Matches(labels)
}

// Set is a map of label:value. It implements Labels.
type Set map[string]string

// String returns all labels listed as a human readable string.
// Conveniently, exactly the format that ParseSelector takes.
func (ls Set) String() string {
	return k8slabels.Set(ls).String()
}

// Has returns whether the provided label exists in the map.
func (ls Set) Has(label string) bool {
	return k8slabels.Set(ls).Has(label)
}

// Get returns the value in the map for the provided label.
func (ls Set) Get(label string) string {
	return k8slabels.Set(ls).Get(label)
}

// Parse takes a string representing a selector and returns a selector
// object, or an error.
func Parse(str string) (Selector, error) {
	sel, err := k8slabels.Parse(str)
	return selector{sel}, err
}
