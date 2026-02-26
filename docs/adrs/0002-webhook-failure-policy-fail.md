# 2. Webhook Failure Policy set to Fail

Date: 2026-02-26

## Status

Accepted

## Context

The `platform-governance-operator` is responsible for enforcing critical security and governance policies on Pods, such as ensuring a read-only root filesystem and non-root execution. Previously, the Admission Webhooks for Pod validation and mutation were configured with `failurePolicy: ignore`.

While `ignore` ensures high availability of the cluster control plane (Pods can still be created even if the operator is down), it presents a severe security risk. If the operator crashes, is unreachable, or fails due to network issues, all Pods are admitted without any security checks or policy enforcement. This silent failure mode invalidates the purpose of a governance operator.

## Decision

We have updated the `failurePolicy` of our core Pod Validating and Mutating Webhooks to `Fail`.

## Consequences

*   **Positive:** Security and governance policies are strictly enforced. No Pod can bypass the `SecurityBaseline` checks or miss the `WorkloadPolicy` defaults.
*   **Negative (Trade-off):** Availability impact. If the `platform-governance-operator` is down or unreachable, the Kubernetes API server will reject new Pod creation requests in the namespaces where the webhook applies. This introduces a hard dependency on the operator's availability for the cluster's normal operation.
*   **Mitigation:** To mitigate the availability risk, the operator must be deployed with high availability (HA) in mind: multiple replicas, anti-affinity rules, proper resource requests/limits, and PodDisruptionBudgets. Leader election is already enabled to support HA deployments safely.