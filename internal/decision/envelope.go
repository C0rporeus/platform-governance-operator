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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SchemaVersion is the version of the serialized Envelope, "<major>.<minor>".
//
// Compatibility rules (ADR-0005):
//   - Within a major version only additions happen: optional fields and new reason codes.
//     A reader accepts any minor of its own major and ignores fields it does not know.
//   - Removing or renaming a field or code, or changing what an outcome means, needs a new major.
//     A reader rejects any other major instead of guessing.
//   - A code unknown to the reader (from a newer minor) is kept and reported by Known() == false;
//     the outcome/violation invariants are still enforced on it.
const SchemaVersion = "1.0"

const schemaMajor = 1

// Subject identifies what was evaluated.
type Subject struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

// PolicyRef pins a policy to the exact revision that was evaluated.
type PolicyRef struct {
	Kind            string `json:"kind"`
	Namespace       string `json:"namespace,omitempty"`
	Name            string `json:"name"`
	UID             string `json:"uid"`
	Generation      int64  `json:"generation"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// Violation is one confirmed rule violation. It can only exist inside a NonCompliant Envelope.
type Violation struct {
	Rule      string   `json:"rule"`
	Control   string   `json:"control,omitempty"`
	Container string   `json:"container,omitempty"`
	Severity  Severity `json:"severity"`
	Message   string   `json:"message"`
	Policy    string   `json:"policy,omitempty"`
}

// Envelope is the canonical result of one evaluation. Its fields are unexported so that the
// only way to build one is through the constructors, which enforce the contract.
type Envelope struct {
	subject Subject
	outcome Outcome
	reason  ReasonCode
	detail  string

	violations []Violation
	policies   []PolicyRef
	digest     string
}

// Option adds optional context to an Envelope.
type Option func(*Envelope)

// WithPolicies records the policies that took part, and the digest of that set.
func WithPolicies(refs ...PolicyRef) Option {
	return func(e *Envelope) {
		e.policies = sortedRefs(refs)
		e.digest = DigestPolicySet(refs)
	}
}

// WithDetail adds a human-readable explanation. It is informational: consumers must not parse it.
func WithDetail(detail string) Option { return func(e *Envelope) { e.detail = detail } }

func build(subject Subject, outcome Outcome, reason ReasonCode, opts []Option) (Envelope, error) {
	e := Envelope{subject: subject, outcome: outcome, reason: reason}
	for _, opt := range opts {
		opt(&e)
	}
	if err := e.validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// NewCompliant reports a complete evaluation with no violation.
func NewCompliant(subject Subject, opts ...Option) (Envelope, error) {
	return build(subject, Compliant, ReasonAllRulesSatisfied, opts)
}

// NewNonCompliant reports confirmed violations. At least one is required.
func NewNonCompliant(subject Subject, violations []Violation, opts ...Option) (Envelope, error) {
	if len(violations) == 0 {
		return Envelope{}, errf("NonCompliant needs at least one violation")
	}
	vs := append([]Violation(nil), violations...)
	sortViolations(vs)
	e := Envelope{subject: subject, outcome: NonCompliant, reason: ReasonRuleViolated, violations: vs}
	for _, opt := range opts {
		opt(&e)
	}
	if err := e.validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// NewNotApplicable reports that no policy governs the subject or that it is exempt.
func NewNotApplicable(subject Subject, reason ReasonCode, opts ...Option) (Envelope, error) {
	return build(subject, NotApplicable, reason, opts)
}

// NewIndeterminate reports an evaluation that cannot tell. It carries no violations by construction.
func NewIndeterminate(subject Subject, reason ReasonCode, opts ...Option) (Envelope, error) {
	return build(subject, Indeterminate, reason, opts)
}

// NewUnavailable reports an evaluation that could not run. It carries no violations by construction.
func NewUnavailable(subject Subject, reason ReasonCode, opts ...Option) (Envelope, error) {
	return build(subject, Unavailable, reason, opts)
}

// Subject returns what was evaluated.
func (e Envelope) Subject() Subject { return e.subject }

// Outcome returns the verdict.
func (e Envelope) Outcome() Outcome { return e.outcome }

// Reason returns the stable reason code.
func (e Envelope) Reason() ReasonCode { return e.reason }

// Detail returns the informational explanation.
func (e Envelope) Detail() string { return e.detail }

// PolicySetDigest returns the digest of the policies evaluated, empty if none were recorded.
func (e Envelope) PolicySetDigest() string { return e.digest }

// Policies returns a copy of the policies evaluated, in canonical order.
func (e Envelope) Policies() []PolicyRef { return append([]PolicyRef(nil), e.policies...) }

// Violations returns a copy of the confirmed violations, in canonical order. It is empty unless
// the outcome is NonCompliant.
func (e Envelope) Violations() []Violation { return append([]Violation(nil), e.violations...) }

// validate enforces the contract; every exit path of the package goes through it.
func (e Envelope) validate() error {
	if !e.outcome.Valid() {
		return errf("unknown outcome %q", e.outcome)
	}
	if e.reason == "" {
		return errf("outcome %s needs a reason code", e.outcome)
	}
	if want, known := e.reason.Outcome(); known && want != e.outcome {
		return errf("reason %s belongs to outcome %s, not %s", e.reason, want, e.outcome)
	}
	if e.outcome != NonCompliant && len(e.violations) > 0 {
		return errf("outcome %s cannot carry violations: only a confirmed NonCompliant can", e.outcome)
	}
	if e.outcome == NonCompliant && len(e.violations) == 0 {
		return errf("NonCompliant needs at least one violation")
	}
	for i, v := range e.violations {
		if v.Rule == "" || v.Message == "" {
			return errf("violation %d needs a rule and a message", i)
		}
		if !v.Severity.Valid() {
			return errf("violation %d has invalid severity %q", i, v.Severity)
		}
	}
	if e.subject.Kind == "" {
		return errf("subject needs a kind")
	}
	for i, p := range e.policies {
		if p.Kind == "" || p.Name == "" || p.UID == "" {
			return errf("policy %d needs kind, name and uid", i)
		}
	}
	return nil
}

// wire is the serialized form.
type wire struct {
	SchemaVersion   string      `json:"schemaVersion"`
	Subject         Subject     `json:"subject"`
	Outcome         Outcome     `json:"outcome"`
	Reason          ReasonCode  `json:"reason"`
	Detail          string      `json:"detail,omitempty"`
	PolicySetDigest string      `json:"policySetDigest,omitempty"`
	Policies        []PolicyRef `json:"policies,omitempty"`
	Violations      []Violation `json:"violations,omitempty"`
}

// MarshalJSON refuses to serialize an Envelope that breaks the contract (including the zero value).
func (e Envelope) MarshalJSON() ([]byte, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(wire{
		SchemaVersion: SchemaVersion, Subject: e.subject, Outcome: e.outcome, Reason: e.reason,
		Detail: e.detail, PolicySetDigest: e.digest, Policies: e.policies, Violations: e.violations,
	})
}

// UnmarshalJSON rejects another major version and anything that breaks the contract, so a
// payload cannot smuggle violations into a non-NonCompliant envelope.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	major, err := parseMajor(w.SchemaVersion)
	if err != nil {
		return err
	}
	if major != schemaMajor {
		return errf("unsupported schema major version %d (this reader handles %d)", major, schemaMajor)
	}
	got := Envelope{
		subject: w.Subject, outcome: w.Outcome, reason: w.Reason, detail: w.Detail,
		violations: w.Violations, policies: w.Policies, digest: w.PolicySetDigest,
	}
	if err := got.validate(); err != nil {
		return err
	}
	if got.digest != "" && got.digest != DigestPolicySet(got.policies) {
		return errf("policySetDigest does not match the listed policies")
	}
	sortViolations(got.violations)
	got.policies = sortedRefs(got.policies)
	*e = got
	return nil
}

func parseMajor(version string) (int, error) {
	maj, min, ok := strings.Cut(version, ".")
	if !ok {
		return 0, errf("malformed schemaVersion %q", version)
	}
	m, err1 := strconv.Atoi(maj)
	_, err2 := strconv.Atoi(min)
	if err1 != nil || err2 != nil || m < 0 {
		return 0, errf("malformed schemaVersion %q", version)
	}
	return m, nil
}

func sortViolations(vs []Violation) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Policy != b.Policy {
			return a.Policy < b.Policy
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Container != b.Container {
			return a.Container < b.Container
		}
		return a.Message < b.Message
	})
}

func sortedRefs(refs []PolicyRef) []PolicyRef {
	out := append([]PolicyRef(nil), refs...)
	sort.SliceStable(out, func(i, j int) bool { return refKey(out[i]) < refKey(out[j]) })
	return out
}

func refKey(p PolicyRef) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%020d", p.Kind, p.Namespace, p.Name, p.UID, p.Generation)
}

// DigestPolicySet is an order-independent digest of the policy revisions evaluated. It covers
// kind, namespace, name, UID and generation; resourceVersion is recorded on each PolicyRef but
// excluded here because status writes change it without changing the policy's meaning.
func DigestPolicySet(refs []PolicyRef) string {
	if len(refs) == 0 {
		return ""
	}
	h := sha256.New()
	for _, r := range sortedRefs(refs) {
		h.Write([]byte(refKey(r)))
		h.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
