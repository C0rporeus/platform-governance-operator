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

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var testSubject = Subject{APIVersion: "v1", Kind: "Pod", Namespace: "app", Name: "web"}

func violation(rule, container string) Violation {
	return Violation{Rule: rule, Control: "ISO/IEC 27001 A.8.2", Container: container, Severity: SeverityHigh, Message: rule + " violated"}
}

func ref(name string, gen int64) PolicyRef {
	return PolicyRef{Kind: "SecurityBaseline", Namespace: "app", Name: name, UID: "uid-" + name, Generation: gen, ResourceVersion: "9"}
}

// Every outcome and every code is covered: adding either without registering it fails here.
func TestEveryReasonCodeIsBoundToADefinedOutcome(t *testing.T) {
	counts := map[Outcome]int{}
	for _, c := range ReasonCodes() {
		o, ok := c.Outcome()
		if !ok || !o.Valid() {
			t.Fatalf("code %s has no valid outcome", c)
		}
		if c != ReasonCode(strings.ToUpper(string(c))) || strings.ContainsAny(string(c), " -") {
			t.Errorf("code %q must be UPPER_SNAKE_CASE", c)
		}
		counts[o]++
	}
	for _, o := range Outcomes() {
		if counts[o] == 0 {
			t.Errorf("outcome %s has no reason code", o)
		}
	}
}

// Golden list: codes are public contract. Removing or renaming one must be a deliberate edit here.
func TestReasonCodesAreStable(t *testing.T) {
	want := []ReasonCode{
		"ALL_RULES_SATISFIED", "RULE_VIOLATED", "NO_MATCHING_POLICY", "NAMESPACE_EXCLUDED", "EXEMPT_UPDATE",
		"UNSUPPORTED_KIND", "PARTIAL_COVERAGE", "MUTATION_UNOBSERVED", "UNSUPPORTED_CONSTRUCT",
		"AMBIGUOUS_TARGET", "POLICY_READ_FAILED", "DEADLINE_EXCEEDED", "DRY_RUN_UNAVAILABLE",
	}
	for _, c := range want {
		if !c.Known() {
			t.Errorf("reason code %s was removed or renamed: breaking change, needs a new major schema version", c)
		}
	}
	if len(ReasonCodes()) != len(want) {
		t.Errorf("a code was added: list it here and bump the minor version (have %d, want %d)", len(ReasonCodes()), len(want))
	}
}

func TestConstructorsEnforceOutcomeReasonAndViolations(t *testing.T) {
	if _, err := NewNonCompliant(testSubject, nil); err == nil {
		t.Error("NonCompliant without violations must fail")
	}
	if _, err := NewIndeterminate(testSubject, ReasonRuleViolated); err == nil {
		t.Error("an Indeterminate envelope must not use a NonCompliant reason")
	}
	if _, err := NewUnavailable(testSubject, ReasonAllRulesSatisfied); err == nil {
		t.Error("an Unavailable envelope must not use a Compliant reason")
	}
	if _, err := NewNotApplicable(testSubject, ""); err == nil {
		t.Error("a reason code is mandatory")
	}
	if _, err := NewNonCompliant(testSubject, []Violation{{Rule: "R", Message: "m", Severity: "Critical"}}); err == nil {
		t.Error("an undefined severity must fail")
	}
	if _, err := NewCompliant(Subject{}); err == nil {
		t.Error("a subject kind is mandatory")
	}
	for name, mk := range map[string]func() (Envelope, error){
		"compliant":     func() (Envelope, error) { return NewCompliant(testSubject) },
		"indeterminate": func() (Envelope, error) { return NewIndeterminate(testSubject, ReasonPartialCoverage) },
		"unavailable":   func() (Envelope, error) { return NewUnavailable(testSubject, ReasonPolicyReadFailed) },
		"n/a":           func() (Envelope, error) { return NewNotApplicable(testSubject, ReasonNoMatchingPolicy) },
	} {
		e, err := mk()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(e.Violations()) != 0 {
			t.Errorf("%s must carry no violations", name)
		}
	}
}

// TASK-07.1.4: the zero value and hand-built payloads cannot publish a violation under another outcome.
func TestIndeterminateCanNeverBeSerializedAsAViolation(t *testing.T) {
	if _, err := json.Marshal(Envelope{}); err == nil {
		t.Error("the zero Envelope must not serialize")
	}
	for _, outcome := range []Outcome{Compliant, Indeterminate, Unavailable, NotApplicable} {
		payload := `{"schemaVersion":"1.0","subject":{"kind":"Pod"},"outcome":"` + string(outcome) + `","reason":"PARTIAL_COVERAGE",` +
			`"violations":[{"rule":"RunAsNonRoot","severity":"High","message":"x"}]}`
		var e Envelope
		err := json.Unmarshal([]byte(payload), &e)
		if err == nil {
			t.Errorf("%s with violations must be rejected", outcome)
		} else if !strings.Contains(err.Error(), "cannot carry violations") && !strings.Contains(err.Error(), "belongs to outcome") {
			t.Errorf("%s: unexpected error %v", outcome, err)
		}
	}
}

func TestRoundTripKeepsEverythingAndIsCanonical(t *testing.T) {
	policies := []PolicyRef{ref("b", 2), ref("a", 1)}
	vs := []Violation{violation("ReadOnlyRootFilesystem", "web"), violation("RunAsNonRoot", "init")}
	e, err := NewNonCompliant(testSubject, vs, WithPolicies(policies...), WithDetail("two rules"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var back Envelope
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e, back) {
		t.Errorf("round trip changed the envelope:\n%+v\n%+v", e, back)
	}
	// Same input in another order: byte-equivalent output (HU-07.1 depends on this).
	e2, _ := NewNonCompliant(testSubject, []Violation{vs[1], vs[0]}, WithPolicies(policies[1], policies[0]), WithDetail("two rules"))
	data2, _ := json.Marshal(e2)
	if string(data) != string(data2) {
		t.Errorf("serialization depends on input order:\n%s\n%s", data, data2)
	}
	if got := e.Violations()[0].Rule; got != "ReadOnlyRootFilesystem" {
		t.Errorf("violations not in canonical order, first is %s", got)
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	e, _ := NewNonCompliant(testSubject, []Violation{violation("RunAsNonRoot", "a")}, WithPolicies(ref("a", 1)))
	e.Violations()[0].Rule = "tampered"
	e.Policies()[0].Name = "tampered"
	if e.Violations()[0].Rule != "RunAsNonRoot" || e.Policies()[0].Name != "a" {
		t.Error("callers must not be able to mutate an envelope")
	}
}

func TestSchemaVersionCompatibility(t *testing.T) {
	base := func(version, extra string) string {
		return `{"schemaVersion":"` + version + `","subject":{"kind":"Pod"},"outcome":"Indeterminate","reason":"PARTIAL_COVERAGE"` + extra + `}`
	}
	var e Envelope
	if err := json.Unmarshal([]byte(base("1.7", `,"futureField":{"x":1}`)), &e); err != nil {
		t.Errorf("a newer minor with unknown fields must be accepted: %v", err)
	}
	for _, v := range []string{"2.0", "0.9", "", "1", "x.y"} {
		if err := json.Unmarshal([]byte(base(v, "")), &e); err == nil {
			t.Errorf("schemaVersion %q must be rejected", v)
		}
	}
	// An unknown code from a newer minor is kept, flagged, and still cannot carry violations.
	if err := json.Unmarshal([]byte(strings.Replace(base("1.3", ""), "PARTIAL_COVERAGE", "BRAND_NEW_CODE", 1)), &e); err != nil {
		t.Fatalf("unknown code from a newer minor: %v", err)
	}
	if e.Reason().Known() {
		t.Error("BRAND_NEW_CODE must report Known() == false")
	}
}

func TestPolicySetDigest(t *testing.T) {
	a, b := ref("a", 1), ref("b", 1)
	if DigestPolicySet([]PolicyRef{a, b}) != DigestPolicySet([]PolicyRef{b, a}) {
		t.Error("digest must not depend on order")
	}
	if DigestPolicySet([]PolicyRef{a}) == DigestPolicySet([]PolicyRef{ref("a", 2)}) {
		t.Error("a new generation must change the digest")
	}
	rv := a
	rv.ResourceVersion = "other"
	if DigestPolicySet([]PolicyRef{a}) != DigestPolicySet([]PolicyRef{rv}) {
		t.Error("resourceVersion must not change the digest")
	}
	if DigestPolicySet(nil) != "" {
		t.Error("empty set has no digest")
	}
	data := `{"schemaVersion":"1.0","subject":{"kind":"Pod"},"outcome":"Compliant","reason":"ALL_RULES_SATISFIED",` +
		`"policySetDigest":"sha256:forged","policies":[{"kind":"SecurityBaseline","name":"a","uid":"u","generation":1}]}`
	var e Envelope
	if err := json.Unmarshal([]byte(data), &e); err == nil {
		t.Error("a digest that does not match its policies must be rejected")
	}
}

func TestTransitions(t *testing.T) {
	for _, to := range Outcomes() {
		if !CanTransition("", to, false) {
			t.Errorf("first observation may be %s", to)
		}
	}
	for _, from := range Outcomes() {
		for _, to := range []Outcome{Indeterminate, Unavailable} {
			if !CanTransition(from, to, false) {
				t.Errorf("%s -> %s must be allowed: the old verdict became stale", from, to)
			}
		}
	}
	for _, from := range []Outcome{Compliant, NonCompliant, NotApplicable} {
		for _, to := range []Outcome{Compliant, NonCompliant, NotApplicable} {
			if !CanTransition(from, to, false) {
				t.Errorf("%s -> %s is a normal change", from, to)
			}
		}
	}
	for _, from := range []Outcome{Indeterminate, Unavailable} {
		for _, to := range []Outcome{Compliant, NonCompliant, NotApplicable} {
			if CanTransition(from, to, false) {
				t.Errorf("%s -> %s needs a fresh, complete evaluation", from, to)
			}
			if !CanTransition(from, to, true) {
				t.Errorf("%s -> %s must be allowed after a complete evaluation", from, to)
			}
		}
	}
	if CanTransition("Bogus", Compliant, true) || CanTransition(Compliant, "Bogus", true) {
		t.Error("undefined outcomes never transition")
	}
}

func TestCombine(t *testing.T) {
	cases := []struct {
		in   []Outcome
		want Outcome
	}{
		{[]Outcome{Compliant, NonCompliant, Unavailable}, NonCompliant},
		{[]Outcome{Compliant, Unavailable, Indeterminate}, Unavailable},
		{[]Outcome{Compliant, Indeterminate}, Indeterminate},
		{[]Outcome{Compliant, NotApplicable}, Compliant},
		{[]Outcome{NotApplicable, NotApplicable}, NotApplicable},
		{nil, NotApplicable},
	}
	for _, c := range cases {
		if got := Combine(c.in...); got != c.want {
			t.Errorf("Combine(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func FuzzUnmarshalNeverBreaksTheContract(f *testing.F) {
	f.Add(`{"schemaVersion":"1.0","subject":{"kind":"Pod"},"outcome":"Compliant","reason":"ALL_RULES_SATISFIED"}`)
	f.Add(`{"schemaVersion":"1.0","subject":{"kind":"Pod"},"outcome":"NonCompliant","reason":"RULE_VIOLATED","violations":[{"rule":"r","severity":"Low","message":"m"}]}`)
	f.Fuzz(func(t *testing.T, payload string) {
		var e Envelope
		if json.Unmarshal([]byte(payload), &e) != nil {
			return
		}
		if e.Outcome() != NonCompliant && len(e.Violations()) > 0 {
			t.Fatalf("accepted violations under %s: %s", e.Outcome(), payload)
		}
		if e.Outcome() == NonCompliant && len(e.Violations()) == 0 {
			t.Fatalf("accepted NonCompliant without violations: %s", payload)
		}
		if _, err := json.Marshal(e); err != nil {
			t.Fatalf("accepted payload cannot be re-serialized: %v", err)
		}
	})
}
