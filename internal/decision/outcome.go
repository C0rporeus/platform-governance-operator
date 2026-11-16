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

// Package decision is the canonical contract for the result of evaluating a workload against
// policy (ENAB-07.1, ADR-0005). It is pure: no Kubernetes client, no I/O. Every evaluation
// path (early validation, Pod admission, continuous audit) expresses its result as an Envelope.
//
// The central guarantee: an ambiguity or a failure to evaluate is never published as a
// violation. Only an Envelope whose Outcome is NonCompliant can hold Violations, and that is
// enforced by the constructors, by MarshalJSON and by UnmarshalJSON, not by convention.
package decision

import (
	"fmt"
	"slices"
)

// Outcome is the verdict of one evaluation.
type Outcome string

const (
	// Compliant: the evaluation was complete and found no violation.
	Compliant Outcome = "Compliant"
	// NonCompliant: the evaluation was complete and confirmed at least one violation.
	NonCompliant Outcome = "NonCompliant"
	// Indeterminate: the evaluation ran but cannot tell (partial coverage, unobservable
	// mutation, unsupported construct). It says nothing about the workload.
	Indeterminate Outcome = "Indeterminate"
	// Unavailable: the evaluation could not run (policies unreadable, deadline, dependency down).
	Unavailable Outcome = "Unavailable"
	// NotApplicable: no policy governs the subject, or the subject is exempt.
	NotApplicable Outcome = "NotApplicable"
)

var allOutcomes = []Outcome{Compliant, NonCompliant, Indeterminate, Unavailable, NotApplicable}

// Outcomes returns every defined outcome, in a stable order.
func Outcomes() []Outcome { return append([]Outcome(nil), allOutcomes...) }

// Valid reports whether o is a defined outcome.
func (o Outcome) Valid() bool {
	return slices.Contains(allOutcomes, o)
}

// Conclusive reports whether o is a verdict about the workload (as opposed to a statement
// about the evaluation itself).
func (o Outcome) Conclusive() bool {
	return o == Compliant || o == NonCompliant || o == NotApplicable
}

// CanTransition reports whether a subject whose last outcome was from may be reported with
// outcome to. A zero from means there is no previous outcome.
//
//   - Any outcome may degrade to Indeterminate or Unavailable: the previous verdict becomes stale
//     and must not be carried forward as if it still held.
//   - Leaving Indeterminate or Unavailable for a conclusive outcome requires a fresh, complete
//     evaluation (freshComplete), never the passing of time or a retry that gave up.
//   - Moving between conclusive outcomes is a normal consequence of the workload or policy changing.
func CanTransition(from, to Outcome, freshComplete bool) bool {
	if !to.Valid() || (from != "" && !from.Valid()) {
		return false
	}
	if from == "" || !to.Conclusive() {
		return true
	}
	if from.Conclusive() {
		return true
	}
	return freshComplete
}

// Combine folds the outcomes of several evaluations of one subject (several policies) into one.
// A confirmed violation stands even if another policy could not be evaluated; otherwise any
// doubt prevents a Compliant verdict; Compliant needs at least one policy that said so.
func Combine(outcomes ...Outcome) Outcome {
	var seen = map[Outcome]bool{}
	for _, o := range outcomes {
		seen[o] = true
	}
	switch {
	case seen[NonCompliant]:
		return NonCompliant
	case seen[Unavailable]:
		return Unavailable
	case seen[Indeterminate]:
		return Indeterminate
	case seen[Compliant]:
		return Compliant
	case seen[NotApplicable]:
		return NotApplicable
	}
	return NotApplicable
}

// outcomeError describes a contract violation caught at runtime.
type outcomeError struct{ msg string }

func (e *outcomeError) Error() string { return "decision: " + e.msg }

func errf(format string, args ...any) error { return &outcomeError{fmt.Sprintf(format, args...)} }
