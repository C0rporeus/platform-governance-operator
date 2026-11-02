# 4. Early validation fidelity through a server-side dry run

Date: 2026-11-02

## Status

Proposed (pending the author's decision on the new `create pods` permission; see PLAN-003 section 5)

## Context

The early workload validation (ADR-0003) evaluates the Pod **template**. Pod admission evaluates the **real Pod**, after every mutating webhook has run. Whatever is added in between is invisible to the early check:

- labels, resources and variables added by this operator's own mutator;
- anything added by third-party mutators, most importantly service mesh sidecar injectors: a sidecar that runs as root or needs `NET_ADMIN` makes the Pods fail at creation while the early check reported the template as compliant;
- labels set by the workload controllers.

Re-implementing every mutator would never keep up. The Kubernetes API can answer the question exactly: a server-side dry run (`dryRun=All`) of a Pod creation goes through the whole admission chain and persists nothing.

## Decision

Add an opt-in fidelity mode, `--early-validation-fidelity=template|dry-run` (default `template`, which is the current behavior and needs no extra privileges). In `dry-run` mode the early validation also creates a synthetic Pod with `DryRunAll` in the workload's namespace, built like the controller would build it (owner references and controller labels per kind) so that policy targeting resolves the same way, and reports the verdict of the whole admission chain.

- Denials from this operator's own webhooks reject in `enforce` mode. Verdicts of third parties (policy engines, quotas, Pod Security Admission) are forwarded as warnings, never as rejections.
- A deadline shorter than the webhook timeout; on expiry or error the early check falls back to `template` mode and the webhook still fails open.

### Safeguards for the new permission

The operator needs `create` on `pods` cluster-wide, and RBAC cannot restrict a verb to dry runs. Without a safeguard that is a privilege-escalation path from a compromised operator, and today the operator cannot create Pods.

1. A `ValidatingAdmissionPolicy` and binding shipped with the operator deny any non-dry-run `create` of Pods by the operator's identity (`request.userInfo.username` and `request.dryRun`). Verified on a real cluster in the spike of PLAN-003.
2. At startup the operator enables `dry-run` only if that policy and its binding exist; otherwise it stays in `template` mode and says so. The safeguard fails closed.
3. Every dry run goes through a single function that always sets `DryRunAll`; a test prevents a `Create` without it.
4. `test/manifests` pins that the identity named in the policy is the service account and namespace Kustomize deploys, and an E2E test proves a real create by that identity is denied.

Requires Kubernetes 1.30 or later (`ValidatingAdmissionPolicy` is GA), already the supported minimum.

## Consequences

*   **Positive:** closes the sidecar/third-party-mutator gap without duplicating anyone's logic; also makes the label and controller-label gaps exact; surfaces Pod Security Admission and quota results at `kubectl apply`.
*   **Positive:** opt-in and reversible; the default keeps today's privileges.
*   **Negative:** a new cluster-wide permission, contained by a policy that only a cluster administrator can change.
*   **Negative:** one more admission round trip per template change (bounded by the existing `matchConditions`), and more moving parts to verify.
*   **Requirement:** the policy identity must stay in sync with the deployed service account (guarded by `test/manifests`).
