# 5. Canonical decision contract

Date: 2026-11-10

## Status

Accepted on 2026-11-10 (ENAB-07.1, `PLAN-005` approved by the author).

## Context

Early validation, Pod admission, dry-run fidelity and continuous audit each report results in their own shape. When an evaluation cannot tell (a mutation it cannot see, coverage cut by a budget, policies it could not read), nothing in the types stops that doubt from being published as a violation, or as a clean result. A violation that is not real destroys trust in every remediation; a clean result that is not real is a false assurance.

## Decision

Package `internal/decision` is the single vocabulary for the result of an evaluation. It is pure (no client, no I/O) so every path can depend on it.

1. **Five outcomes**: `Compliant`, `NonCompliant`, `Indeterminate`, `Unavailable`, `NotApplicable`. `Indeterminate` and `Unavailable` are statements about the evaluation, never about the workload.
2. **Only a confirmed `NonCompliant` holds violations.** `Envelope` has unexported fields and is built through constructors; `MarshalJSON` and `UnmarshalJSON` re-validate, so neither a bug nor a payload can put a violation under another outcome. The zero value does not serialize.
3. **Stable reason codes**, each bound to exactly one outcome in a registry (`ALL_RULES_SATISFIED`, `RULE_VIOLATED`, `NO_MATCHING_POLICY`, `PARTIAL_COVERAGE`, `POLICY_READ_FAILED`, …). A golden test fails when a code is removed, renamed or added without a deliberate edit.
4. **Pinned inputs**: subject, and for each policy kind/namespace/name/UID/generation/resourceVersion, plus a policy-set digest that is order-independent and covers UID and generation but not resourceVersion (status writes change it without changing meaning).
5. **Transitions**: any outcome may degrade to `Indeterminate`/`Unavailable` (the old verdict is stale and is not carried forward); leaving them for a conclusive outcome needs a fresh, complete evaluation. `Combine` folds several policies: a confirmed violation stands; otherwise any doubt prevents `Compliant`.
6. **Canonical serialization**: violations and policies are sorted, so equal inputs give byte-equal output (required by HU-07.1).

### Versioning (`SchemaVersion = "1.0"`)

- Within a major version only additions: optional fields and new reason codes. Readers accept any minor of their major and ignore unknown fields; an unknown code is kept with `Known() == false` and the outcome/violation invariants still apply.
- Removing or renaming a field or code, or changing the meaning of an outcome, requires a new major; readers reject other majors rather than guess.
- Any change of semantics requires a new ADR and compatibility fixtures.

## Consequences

- Nothing is rewired yet: `Governor`, the webhooks and the audit keep their current types. HU-07.1 moves them onto the envelope and adds differential tests across paths; until then the contract is enforced and tested in isolation.
- Fail-closed on unavailability (ADR-0002) is unchanged, but its rejection is reported as `Unavailable`, not as a workload violation.
- Each new path or rule must pick a reason code up front; ambiguity has a named home that is not a violation.
