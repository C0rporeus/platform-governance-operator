# 3. Early workload validation fails open

Date: 2026-10-28

## Status

Accepted (2026-11-02), together with design decisions D1–D6 of PLAN-002.

## Context

HU-02.3 adds a validating webhook on `apps/v1` `Deployment` and `StatefulSet` that evaluates `spec.template` with the same rules as the Pod webhook, so that `kubectl apply` reports violations immediately instead of leaving a ReplicaSet that cannot create Pods.

ADR-0002 set `failurePolicy: Fail` for the Pod webhooks, because a governance operator that admits everything when it is down gives false assurance. The same reasoning does not transfer to the workload webhook: the Pod is validated again on creation, so the workload webhook is early feedback, not the security boundary.

Using `Fail` there would add cluster-wide availability coupling for little gain, and would make it impossible to apply a corrected operator Deployment while the operator is down (the update itself would be rejected by the webhook that is down).

## Decision

The `Deployment`/`StatefulSet` validating webhooks use `failurePolicy: Ignore` with a short `timeoutSeconds`. The Pod webhooks keep `Fail` (ADR-0002 is unchanged).

The webhook only evaluates when `spec.template` changes, never on `DELETE` or subresources, and ships in `warn` mode by default (`--early-validation-mode=off|warn|enforce`).

## Consequences

*   **Positive:** an operator outage does not block workload rollouts or the repair of the operator itself. Protection is unchanged because Pod admission still enforces every baseline.
*   **Positive:** `warn` by default makes the rollout non-disruptive and consistent with the Enforce/Warn/Audit model.
*   **Negative:** while the operator is unavailable the early feedback is lost; violations surface later, at Pod creation.
*   **Negative:** `DaemonSet`, `Job`, `CronJob` and third-party workload kinds get no early feedback.
*   **Requirement:** both paths must share one evaluation implementation and be covered by a parity test, otherwise `enforce` could reject a workload whose Pods would be admitted.

See `PLAN-002` in the `0.docs` workspace for the full design and risk register.

## Related decisions (2026-11-02)

*   Minimum supported Kubernetes version is **1.30** (CRD validation rules and the `quantity` library are stable, CRD validation ratcheting is on by default).
*   CRD invariants move to CEL; the CRD validating webhooks stay only for what CEL cannot check (structural validity of `LabelSelector`).
*   `ValidatingAdmissionPolicy` for early validation is deferred: it cannot reuse the Go rules without duplicating them.
*   The default mode stays `warn`; whether to switch to `enforce` is reviewed when EPIC-03 closes.
