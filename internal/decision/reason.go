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

package decision

// ReasonCode is a stable, machine-readable explanation of an outcome. Codes are part of the
// public contract: they are never renamed, reused for another meaning or removed within a
// major schema version; new ones are added with a minor version.
type ReasonCode string

// Severity ranks a confirmed violation.
type Severity string

const (
	SeverityLow    Severity = "Low"
	SeverityMedium Severity = "Medium"
	SeverityHigh   Severity = "High"
)

// Valid reports whether s is a defined severity.
func (s Severity) Valid() bool { return s == SeverityLow || s == SeverityMedium || s == SeverityHigh }

const (
	// Compliant.
	ReasonAllRulesSatisfied ReasonCode = "ALL_RULES_SATISFIED"

	// NonCompliant.
	ReasonRuleViolated ReasonCode = "RULE_VIOLATED"

	// NotApplicable.
	ReasonNoMatchingPolicy  ReasonCode = "NO_MATCHING_POLICY"
	ReasonNamespaceExcluded ReasonCode = "NAMESPACE_EXCLUDED"
	ReasonExemptUpdate      ReasonCode = "EXEMPT_UPDATE"
	ReasonUnsupportedKind   ReasonCode = "UNSUPPORTED_KIND"

	// Indeterminate.
	ReasonPartialCoverage      ReasonCode = "PARTIAL_COVERAGE"
	ReasonMutationUnobserved   ReasonCode = "MUTATION_UNOBSERVED"
	ReasonUnsupportedConstruct ReasonCode = "UNSUPPORTED_CONSTRUCT"
	ReasonAmbiguousTarget      ReasonCode = "AMBIGUOUS_TARGET"

	// Unavailable.
	ReasonPolicyReadFailed  ReasonCode = "POLICY_READ_FAILED"
	ReasonDeadlineExceeded  ReasonCode = "DEADLINE_EXCEEDED"
	ReasonDryRunUnavailable ReasonCode = "DRY_RUN_UNAVAILABLE"
)

// reasonOutcome binds every code to the single outcome it may accompany. This table is the
// registry; a unit test fails if a constant is added without being listed here.
var reasonOutcome = map[ReasonCode]Outcome{
	ReasonAllRulesSatisfied:    Compliant,
	ReasonRuleViolated:         NonCompliant,
	ReasonNoMatchingPolicy:     NotApplicable,
	ReasonNamespaceExcluded:    NotApplicable,
	ReasonExemptUpdate:         NotApplicable,
	ReasonUnsupportedKind:      NotApplicable,
	ReasonPartialCoverage:      Indeterminate,
	ReasonMutationUnobserved:   Indeterminate,
	ReasonUnsupportedConstruct: Indeterminate,
	ReasonAmbiguousTarget:      Indeterminate,
	ReasonPolicyReadFailed:     Unavailable,
	ReasonDeadlineExceeded:     Unavailable,
	ReasonDryRunUnavailable:    Unavailable,
}

// Known reports whether c is registered in this version of the contract.
func (c ReasonCode) Known() bool { _, ok := reasonOutcome[c]; return ok }

// Outcome returns the outcome c accompanies, and false for an unknown code.
func (c ReasonCode) Outcome() (Outcome, bool) { o, ok := reasonOutcome[c]; return o, ok }

// ReasonCodes returns every registered code, unordered.
func ReasonCodes() []ReasonCode {
	out := make([]ReasonCode, 0, len(reasonOutcome))
	for c := range reasonOutcome {
		out = append(out, c)
	}
	return out
}
