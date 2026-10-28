/*
Copyright 2026.

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

// Package policy resolves which governance policies apply to a workload.
// It is pure in-memory logic (no API calls) so it can run inside admission
// handlers within the p99 latency budget.
package policy

import (
	"slices"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// Specificity ranks how narrowly a policy is attached. Higher wins.
type Specificity int

const (
	// SpecificityCluster is a cluster-scoped policy.
	SpecificityCluster Specificity = iota
	// SpecificityNamespace is a namespaced policy without targeting.
	SpecificityNamespace
	// SpecificitySelector is a namespaced policy with a label selector.
	SpecificitySelector
	// SpecificityTargetRef is a namespaced policy with a targetRef.
	SpecificityTargetRef
)

// Subject describes the workload being evaluated.
type Subject struct {
	// Refs are the resources a targetRef may match: the object itself and its
	// owners (for example a Pod's ReplicaSet and Deployment).
	Refs []corev1alpha1.PolicyTargetReference
	// Labels are the labels of the object being evaluated.
	Labels map[string]string
}

// Candidate is a policy under consideration, stripped of its concrete type.
type Candidate struct {
	Name      string
	Namespace string
	// ClusterScoped marks policies that come from a Cluster* resource.
	ClusterScoped bool
	Priority      int32
	Targeting     corev1alpha1.PolicyTargeting
}

// SpecificityOf returns the specificity of a candidate. A targetRef outranks a
// selector when both are set, matching the CRD contract.
func SpecificityOf(c Candidate) Specificity {
	switch {
	case c.ClusterScoped:
		return SpecificityCluster
	case c.Targeting.TargetRef != nil:
		return SpecificityTargetRef
	case c.Targeting.Selector != nil:
		return SpecificitySelector
	default:
		return SpecificityNamespace
	}
}

// Matches reports whether the candidate applies to the subject. An invalid
// selector returns an error and never matches.
func Matches(c Candidate, s Subject) (bool, error) {
	if ref := c.Targeting.TargetRef; ref != nil && !c.ClusterScoped {
		return containsRef(s.Refs, *ref), nil
	}
	if sel := c.Targeting.Selector; sel != nil {
		parsed, err := metav1.LabelSelectorAsSelector(sel)
		if err != nil {
			return false, err
		}
		return parsed.Matches(labels.Set(s.Labels)), nil
	}
	return true, nil
}

// Resolve filters candidates to those matching the subject and orders them
// from most to least specific. Ties break by higher Priority, then by
// namespace and name so the result is deterministic. Candidates with an
// invalid selector are skipped and reported in the returned errors.
func Resolve(candidates []Candidate, s Subject) ([]Candidate, []error) {
	var (
		matched []Candidate
		errs    []error
	)
	for _, c := range candidates {
		ok, err := Matches(c, s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok {
			matched = append(matched, c)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if sa, sb := SpecificityOf(a), SpecificityOf(b); sa != sb {
			return sa > sb
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return matched, errs
}

func containsRef(refs []corev1alpha1.PolicyTargetReference, want corev1alpha1.PolicyTargetReference) bool {
	return slices.Contains(refs, want)
}
