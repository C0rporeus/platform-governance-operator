# Designing Platform Contracts with CRDs

*By f3nr1r - 2026-02-26*

## The Problem with "YOLO" Kubernetes

As organizations scale their Kubernetes footprint, a common pattern emerges: development teams are handed namespace access, and they immediately begin deploying workloads. While this self-service model is the ultimate goal of Kubernetes, without guardrails, it quickly degrades into what I call "YOLO Kubernetes."

- Workloads run as `root` because it's the default in many base images.
- Resource `requests` and `limits` are omitted, leading to noisy neighbor problems and node starvation.
- Mandatory organizational labels (like `cost-center` or `team-owner`) are forgotten, making showback/chargeback impossible.
- Observability is an afterthought, and sidecars or environment variables are inconsistently applied.

The traditional approach to solving this is relying on CI/CD pipelines to enforce checks (e.g., using Conftest or OPA in the pipeline). However, CI/CD is easily bypassed. The true source of truth in Kubernetes is the API server.

## Enter Platform Contracts

To move from an unmanaged cluster to a true Internal Developer Platform (IDP), we need **Platform Contracts**. A platform contract is a declarative agreement between the platform team and the development teams about how workloads must behave.

Instead of writing endless wiki pages, we encode these contracts directly into the cluster using Custom Resource Definitions (CRDs).

### The Platform Governance Operator

In my latest project, the `Platform Governance Operator`, I demonstrate how to build an active enforcement engine using Go and `controller-runtime`.

Instead of generic Open Policy Agent (OPA) rules which can be difficult to test and maintain at scale, a dedicated Operator offers:
1. **Strongly typed contracts** (CRDs) like `SecurityBaseline`, `WorkloadPolicy`, and `TelemetryProfile`.
2. **Mutating Webhooks** that actively *fix* non-compliant workloads (e.g., injecting default resource limits or tracing environment variables).
3. **Validating Webhooks** that reject dangerous payloads (e.g., rejecting Pods trying to run as root).

### Example: The WorkloadPolicy

By defining a `WorkloadPolicy` CRD, the platform team can specify:
```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: WorkloadPolicy
metadata:
  name: standard-policy
spec:
  mandatoryLabels:
    - cost-center
    - owner
  defaultRequests:
    cpu: 100m
    memory: 128Mi
```
When a developer submits a Pod without resource requests, our Mutating Webhook intercepts the request, reads the active `WorkloadPolicy`, and silently injects the defaults. If they miss a mandatory label, the webhook injects a `default-value-required` placeholder (or rejects it via validation).

## Conclusion

By shifting governance from "documentation and CI checks" to "active cluster-level enforcement via CRDs," you dramatically reduce developer cognitive load while ensuring a secure, resilient, and observable platform.

*In the next article, we'll dive deep into writing Mutating Webhooks in Go, and how to test them reliably using `envtest`.*
