# CLAUDE.md

This file provides guidance to Claude Code when working with the `platform-governance-operator` codebase.

## Overview

A **Kubernetes Operator** that enforces platform governance through four Custom Resource Definitions (CRDs) — `SecurityBaseline`, `ClusterSecurityBaseline`, `WorkloadPolicy`, `TelemetryProfile` — and their admission webhooks (Pod validating/mutating plus per-CRD validating/defaulting). It acts as a security and standards enforcement layer over any Kubernetes cluster.

- **Domain:** `platform.f3nr1r.io`
- **Group:** `core.platform.f3nr1r.io`
- **API Version:** `v1alpha1`
- **Module:** `github.com/f3nr1r/platform-governance-operator`
- **Go:** 1.25.0 minimum in `go.mod` (CI matrix: `1.25` + `stable`; local dev verified on 1.27.3) | **controller-runtime:** v0.23.1 | **k8s.io/api:** v0.35.0

## Repository Structure

```
cmd/main.go                         Manager entry point (registers controllers + webhooks)
api/v1alpha1/                       CRD type definitions (kubebuilder markers live here)
  securitybaseline_types.go
  clustersecuritybaseline_types.go  Cluster-scoped baseline: embeds SecurityBaselineSpec + namespaceSelector
  workloadpolicy_types.go
  telemetryprofile_types.go
  policy_target_types.go            PolicyTargetReference + PolicyTargeting (embedded in all 3 specs)
  zz_generated.deepcopy.go          AUTO-GENERATED — never edit
internal/controller/                Reconciliation logic (one file per CRD)
  securitybaseline_controller.go
  clustersecuritybaseline_controller.go
  workloadpolicy_controller.go      Includes HPA lifecycle management
  telemetryprofile_controller.go
  reconcile_status_helper.go        Shared status condition helpers
  reconcile_status_helper_test.go   Unit tests for unexported status helper
  workloadpolicy_hpa_test.go        Unit tests for unexported HPA helpers
internal/policy/                    Pure in-memory policy resolution (no API calls)
  resolver.go                       Specificity ordering: targetRef > selector > namespace > cluster; generic Select()
  subject.go                        SubjectForPod (Pod + owners + Deployment derived from ReplicaSet) / SubjectForDeployment
  cluster.go                        DefaultExcludedNamespaces, IsClusterExcluded, NamespaceMatches
  validate.go                       ValidateTargeting (used by the CRD validating webhooks)
  index.go                          Field index `spec.policyScope` (registered in cmd/main.go)
internal/webhook/
  v1alpha1/                         CRD defaulting/validating webhooks
  core/                             Core API (Pod) webhooks
    governor.go                     Governor: the single baseline selection + evaluation path (Pod and workload webhooks)
    pod_webhook.go                  Validating webhook (the security boundary, fails closed)
    pod_mutating_webhook.go         Mutating webhook
    workload_webhook.go             Early validation of Deployment/StatefulSet templates (feedback, fails open)
config/
  crd/bases/                        AUTO-GENERATED — never edit
  rbac/role.yaml                    AUTO-GENERATED — never edit
  webhook/manifests.yaml            AUTO-GENERATED — never edit
  samples/                          Example CRs — edit freely
  default/                          Kustomization entry point
docs/adrs/                          Architecture Decision Records
docs/GOOD-PRACTICES-EVIDENCE.md       Evidence document: sources, practices, design decisions, deviations, traceability (update when a decision changes)
test/
  integration/                      envtest-based integration tests (included in make test)
    suite_test.go                   envtest bootstrap (shared k8sClient, ctx)
    controller_test_helpers.go      expectAvailableCondition shared helper
    *_controller_test.go            Reconcile() smoke tests via public API (3 files)
    workloadpolicy_hpa_test.go      HPA lifecycle integration tests (12 specs)
  e2e/                              End-to-end tests (requires Kind cluster)
  utils/                            Shared test utilities (envtest binary discovery)
Makefile                            All build/test/deploy commands
PROJECT                             Kubebuilder metadata — never edit
```

**Never edit auto-generated files:** `config/crd/bases/*.yaml`, `config/rbac/role.yaml`, `config/webhook/manifests.yaml`, `**/zz_generated.*.go`, `PROJECT`.

**Never remove scaffold markers:** `// +kubebuilder:scaffold:*` comments — the CLI injects code at these points.

## Commands

### Development

```bash
make manifests    # Regenerate CRDs, RBAC, webhook configs from +kubebuilder markers
make generate     # Regenerate DeepCopy methods (zz_generated.deepcopy.go)
make fmt          # Run gofmt
make vet          # Run go vet
make build        # Compile binary → bin/manager
make run          # Run controller locally with current kubeconfig (no docker)
```

### Testing

```bash
make test                        # All tests except e2e (internal/ + test/integration/)
make test-e2e                    # E2E tests on Kind cluster
make setup-test-e2e              # Create Kind cluster (name: platform-governance-operator-test-e2e)
make cleanup-test-e2e            # Delete Kind cluster
go test ./internal/...           # Unit tests (unexported-function coverage)
go test ./test/integration/...   # Integration tests only (envtest + black-box)
go test ./internal/webhook/...   # Webhook handler tests only
```

### Test layer architecture

| Layer | Location | Tools | Tests |
|---|---|---|---|
| Unit (private) | `internal/controller/*_test.go` | `go test` + fake/real client | Unexported helpers: HPA builders, status helper |
| Unit (webhooks) | `internal/webhook/*/` | `go test` + fake client | Pod validator & mutator handlers |
| Unit (policy) | `internal/policy/` | `go test` + fake indexer | Specificity resolution, index keys |
| Integration | `test/integration/` | Ginkgo + envtest | Reconcile() smoke + HPA lifecycle (public API only) |
| E2E | `test/e2e/` | Ginkgo + Kind cluster | Full deployment (build tag `//go:build e2e`) |

**Rule:** `internal/controller/` only keeps `_test.go` files that test **unexported** symbols. Tests for exported behaviour go in `test/integration/`.

### Linting

```bash
make lint          # golangci-lint v2.15.0 (19 linters; must report 0 issues)
make lint-fix      # Auto-fix style issues
make lint-config   # Verify linter config
```

### Docker & Deployment

```bash
make docker-build IMG=<registry>/<image>:<tag>
make docker-push  IMG=<registry>/<image>:<tag>
make docker-buildx IMG=<registry>/<image>:<tag>   # Multi-arch: linux/arm64,amd64,s390x,ppc64le

make install      # Install CRDs into current cluster
make deploy IMG=<registry>/<image>:<tag>
make build-installer                              # Generate consolidated install.yaml
make uninstall    # Remove CRDs from cluster
make undeploy     # Remove controller deployment
```

### CI (`.github/workflows/ci.yml`)

`lint` (parallel) + `test` (parallel) → `build` (requires both)

## Architecture

### Three CRDs as Platform Contracts

| CRD | Webhook Type | Enforcement |
|---|---|---|
| `SecurityBaseline` | Validating (Pod) | Denies Pods violating security constraints |
| `WorkloadPolicy` | Mutating (Pod) + HPA controller | Injects labels/resources, manages HPAs |
| `TelemetryProfile` | Mutating (Pod) | Injects OpenTelemetry env vars |

### Admission Webhook Pipeline

```
Pod Admission Request
        ↓
[Mutating Webhook /mutate-core-v1-pod]
  → Apply highest-priority WorkloadPolicy (default labels, resource requests)
  → Apply highest-priority TelemetryProfile (OTEL env vars)
        ↓
[Validating Webhook /validate-core-v1-pod]
  → Check SecurityBaseline constraints (runAsNonRoot, readOnlyRootFilesystem)
  → Deny if any violation found
        ↓
Persisted to etcd
```

**Failure Policy: `fail`** — Pods are rejected if the operator is unavailable. This is a deliberate security decision (ADR-0002). Mitigated by HA deployment (multiple replicas, anti-affinity, PodDisruptionBudget).

Webhook registration is **imperative** for core/v1 Pod resources (registered in `cmd/main.go`), **declarative** for CRD resources (via kubebuilder markers → `make manifests`).

### Controller Responsibilities

- **SecurityBaselineReconciler** — Sets `Available` status. Enforcement is entirely in the validating webhook.
- **WorkloadPolicyReconciler** — Sets `Available` status + manages HPA lifecycle:
  - Only the **highest-priority** WorkloadPolicy creates HPAs for a given Deployment
  - Per-Deployment opt-in/out via annotation `core.platform.f3nr1r.io/hpa-enabled: "true"|"false"`
  - Managed HPAs carry label `core.platform.f3nr1r.io/managed-hpa: "true"` and owner reference
- **TelemetryProfileReconciler** — Sets `Available` status. Enforcement is entirely in the mutating webhook.

### Policy Priority System

- Both `WorkloadPolicy` and `TelemetryProfile` have a `priority` integer field
- Policies sorted descending — highest priority wins
- Label/resource injection is **conservative**: never overwrites already-set values

### HPA Management (WorkloadPolicy)

```yaml
spec:
  hpa:
    enabledByDefault: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
```

Per-Deployment annotation overrides `enabledByDefault`:
```yaml
metadata:
  annotations:
    core.platform.f3nr1r.io/hpa-enabled: "false"
```

## Key Conventions

### Kubebuilder Markers

All RBAC, validation, defaulting, and subresource declarations live as Go comments:
```go
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=workloadpolicies,verbs=get;list;watch
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:Required
// +kubebuilder:default="value"
```
Run `make manifests` after any marker change to regenerate YAML.

### Controller Pattern

```go
type FooReconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    Recorder record.EventRecorder
}

func (r *FooReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
// Returns: ctrl.Result{Requeue: true} or ctrl.Result{RequeueAfter: d} or ctrl.Result{}
```

### Webhook Pattern

```go
// Validating
type PodValidator struct{ Client client.Client; decoder admission.Decoder }
func (v *PodValidator) Handle(ctx context.Context, req admission.Request) admission.Response
// Returns: admission.Allowed(), admission.Denied("reason"), admission.Errored(code, err)

// Mutating
type PodMutator struct{ Client client.Client; decoder admission.Decoder }
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response
// Mutations applied as JSON patch via admission.PatchResponseFromRaw()
```

### Status Conditions

```go
meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
    Type:               "Available",
    Status:             metav1.ConditionTrue,
    Reason:             "Reconciled",
    Message:            "...",
    ObservedGeneration: obj.Generation,
})
```

### Event Recording

```go
r.Recorder.Event(obj, corev1.EventTypeNormal, "Reconciled", "message")
r.Recorder.Event(obj, corev1.EventTypeWarning, "PodDenied", "reason")
```

### Logging

```go
log := logf.FromContext(ctx)
log.Info("message", "key", value)
log.Error(err, "message", "key", value)
log.V(1).Info("verbose message")  // Debug level
```

### Testing Pattern (Ginkgo + Gomega)

```go
var _ = Describe("FooController", func() {
    It("should reconcile", func() {
        // envtest provides real API server + etcd
        Expect(k8sClient.Create(ctx, obj)).To(Succeed())
        Eventually(func() bool { ... }).Should(BeTrue())
    })
})
```

- `test/integration/suite_test.go` — global envtest setup, shared `k8sClient`, `ctx`
- `test/integration/controller_helpers_test.go` — `controllerSmokeBody(spec)` shared Reconcile smoke test; a new CRD adds a ~20-line spec file instead of copying one
- `internal/webhook/v1alpha1/webhook_suite_test.go` — webhook suite setup
- CRDs loaded from `config/crd/bases/` by envtest (requires `make manifests` first)
- Black-box tests import the controller package: `controller.WorkloadPolicyReconciler{...}`

## End-to-end tests (`test/e2e/`, build tag `e2e`)

- `make test-e2e` creates a disposable 3-node Kind cluster (`test/e2e/kind-config.yaml`) on Docker, runs the suite and deletes it; `E2E_KEEP=true` keeps it, `make test-e2e-run` re-runs against it, `make cleanup-test-e2e` deletes it. Kind is pinned (`KIND_VERSION`) and installed in `bin/`.
- **Isolation:** the cluster has its own kubeconfig (`bin/e2e-kubeconfig`); `~/.kube/config` is never read or modified. `BeforeSuite` aborts unless `KUBECONFIG` is that file and the context is `kind-$(KIND_CLUSTER)`. Never point the suite at a shared cluster: it installs `failurePolicy: Fail` webhooks and cluster-wide baselines.
- The operator is deployed once in `BeforeSuite` (Ginkgo may reorder top-level containers). Each `Describe` uses its own namespaces and deletes every cluster-scoped object it creates (`DeferCleanup`).
- Test Pods use `registry.k8s.io/pause:3.10` (preloaded on Kind nodes, runs as non-root) and server-side dry runs where nothing needs to persist.
- Scenarios: denial message (rule, ISO control, policy), events in workload namespaces, Warn + dry-run without events, metadata-only updates, defaults/telemetry injection, managed HPA and opt-out, cluster baseline inheritance and exclusion, HA (2 replicas on distinct nodes, PDB, replica loss without fail-open, self-restart and cert-manager restart under a lockdown baseline).

## Development rule

**A defect found is fixed in the same change, not deferred.** Fix it with a regression test that fails without the fix, run `make test` and `make lint`, then record it as done. Defer only when the fix needs a decision or input that is not available, and say what blocks it.

## Pod admission semantics

- **Validator** (`vpod.kb.io`, `create;update`): evaluates on create. On update it allows requests that change only fields Pod Security admission exempts (metadata other than the seccomp/AppArmor annotations, `activeDeadlineSeconds`, `tolerations`), so a running Pod that violates a newer baseline can still be relabelled. Any other change (for example an image) is evaluated in full.
- **Mutator** (`mpod.kb.io`, `create` only): defaults apply at creation. The spec of a running Pod is immutable, so a patch on update would be rejected by the API server.
- **Mirror Pods** of static Pods are skipped only when the request carries the `kubernetes.io/config.mirror` annotation **and** a node identity (`system:node:*` user in group `system:nodes`). The annotation alone is user-settable and would bypass every baseline.
- **Windows Pods** (`spec.os.name: windows`) skip the Linux-only rules: privilege escalation, seccomp, capabilities and read-only root filesystem. `runAsNonRoot` still applies.
- **Dry-run** requests record no events (`sideEffects: None`).
- Each violation prints an ISO/IEC 27001 control: `A.8.2` (privileged access rights) for non-root, privilege, capabilities and seccomp; `A.8.9` (configuration management) for read-only root filesystem, host namespaces and `hostPath`; `A.5.21` (ICT supply chain) for image rules. Titles come from a secondary summary of the standard and the mapping awaits confirmation by the ISMS owner (see `docs/GOOD-PRACTICES-EVIDENCE.md`).

## Availability invariants

`test/manifests` fails if any of these regress: every webhook has `timeoutSeconds` between 1 and 10; the Pod webhooks fail closed and the early workload validation fails open; the Pod webhooks exclude `kube-system`, `kube-public`, `kube-node-lease`, `cert-manager` and the operator namespace by the immutable `kubernetes.io/metadata.name` label, and that namespace equals `namespace:` in `config/default/kustomization.yaml`; the manager runs at least 2 replicas with anti-affinity and a PodDisruptionBudget; the memory limit equals the request.

## Policy Targeting

Every policy kind embeds `PolicyTargeting` (`targetRef` | `selector`; neither = whole namespace).

- **Precedence:** `targetRef` > `selector` > namespace-wide > cluster; ties break by `priority` (higher wins), then namespace/name. A policy with both fields set is treated as `targetRef`.
- **Pods:** `PodValidator`/`PodMutator` call `policy.Select(items, policy.SubjectForPod(pod), ...)`. The Deployment ref is derived from the ReplicaSet owner only if the RS name ends with the Pod's `pod-template-hash` label; otherwise no Deployment ref is produced (a `targetRef` to that Deployment then does not match).
- **Selectors** match Pod labels at admission and the Deployment's *pod template* labels in the HPA reconciler.
- **HPA ownership is per Deployment**, not per namespace: the most specific `WorkloadPolicy` with `horizontalScaling` governs each Deployment. When a Deployment stops being governed by any policy, the policy that created its managed HPA deletes it; unmanaged HPAs are never touched.
- **Invalid selectors** are rejected by the CRD validating webhooks (`ValidateTargeting`); if one still reaches admission (webhooks disabled) it is logged and skipped.
- The `spec.policyScope` field index is registered but admission currently lists from the namespace-scoped informer cache and filters in memory; use the index if lists ever become large.

## Early workload validation (`workload_webhook.go`, ADR-0003)

`vworkload.kb.io` evaluates the Pod template of a `Deployment`, `StatefulSet`, `DaemonSet`, `Job` or `CronJob` (a CronJob's is `spec.jobTemplate.spec.template`) with the same code Pod admission uses, so `kubectl apply` reports a violation instead of leaving a ReplicaSet that cannot create Pods.

- **Feedback, not the boundary:** `failurePolicy: Ignore`, `timeoutSeconds: 3`. If the operator is down or policies cannot be read the apply goes through, and every Pod is still validated by `vpod.kb.io` (`Fail`). `test/manifests` pins both policies.
- **The Pod as mutation leaves it:** Pod admission validates the Pod *after* the mutating webhook, so the early check applies the same defaults to the template first (`Defaulter.Apply`, shared with `PodMutator`: mandatory labels, resource defaults, OTel variables). Policies are selected with the labels before mutation, as the mutator does, and baselines with the labels after it, as the validator does. A `selector` on a label that only `mandatoryLabels` adds therefore matches in both. `TestEarlyValidationSeesTheLabelsThePodMutatorAdds` runs a Pod through the real mutator (applying its JSON patch) and the real validator and compares.
- **One evaluation path (D2):** `Governor.Evaluate(ns, syntheticPod, subject)`. The Pod is built from the template (`podFromTemplate`: no name, no owners) and the `Subject` from the workload (`policy.SubjectForDeployment` / `SubjectForStatefulSet`). `TestEarlyValidationAndPodAdmissionAgree` fails if the two webhooks ever disagree on the same spec.
- **Only when the template changes (D3):** `UPDATE` with an equal Pod template is admitted untouched (the controllers keep updating replicas, annotations and status of workloads that already violate a policy). `DELETE` and objects with `deletionTimestamp` are skipped. The API server also filters with a `matchCondition` (`config/webhook/patches/namespace-selector.yaml`, per kind: a CronJob compares its job template), so the webhook is not even called for those updates. A Job's template is immutable, so it is only ever evaluated on create. A `targetRef` cannot select a CronJob's Pods (they belong to the Jobs it creates), which is why the CRD rejects a CronJob `targetRef`.
- **Mode (D4, `--early-validation-mode`):** `warn` (default) returns the violations as `Warning:` lines (`[early]` prefix). The API server accepts 4096 characters of warnings per response, truncates each to 256 beyond that and drops the rest, so the report groups violations by (policy, rule) with the affected containers, keeps every line under 240 characters and the total under 3500, and ends with a "... and N more kinds of violation" line when something was left out (`early_report.go`). Events carry the whole group `enforce` rejects only `Enforce` violations; `Warn`/`Audit` baselines only warn or log in both modes. `off` does not register the handler. Violations also go to the API server audit log (`violations`, `policies` annotations) and, except on dry runs, to events (`WorkloadDenied`, `WorkloadViolationWarn`, `WorkloadViolationAudit`).
- **Known gaps, tracked as `HU-02.4` (PLAN-003, `0.docs/plans/`):** the early check evaluates the template, not the real Pod. It sees what this operator's own mutator adds (`Defaulter`, see below) but not what third-party mutators add (mesh sidecars; `dry-run` fidelity, `TASK-02.6.5`..`02.6.7`, ADR-0004 proposed), the Pod name or ReplicaSet hash (a `targetRef` to a `Pod` or `ReplicaSet` can never match early, `TASK-02.6.8`),  Pod admission still covers all of it. Workloads that were already running before a policy are never re-evaluated until `HU-02.5` (continuous audit).
- Same namespace exclusions as the Pod webhooks (the operator namespace and `cert-manager` can never be held up by it).

## CRD validation rules (CEL) and size limits

The API server enforces these on every write (minimum Kubernetes 1.30; CRD validation ratcheting means a stored object is only re-checked on the fields an update changes). They live in the type markers, not in webhooks; the CRD webhooks keep only what CEL cannot express (structural validity of a `LabelSelector`, quantity format).

| Type | Rule |
|---|---|
| `HorizontalScalingPolicy` | `maxReplicas >= minReplicas` (defaults 2 and 10 are applied before the rule runs) |
| `PolicyTargetReference` | the kind must belong to a *known* group: core is `Pod`/`ReplicationController`, `apps` is `Deployment`/`StatefulSet`/`DaemonSet`/`ReplicaSet`, `batch` is `Job`; other groups (for example `argoproj.io`) are free. `CronJob` is rejected on purpose: it owns Jobs, not Pods, so it could never match (see `internal/policy/subject.go`). A kind without its group (`Deployment` with no `apps`) is rejected because matching is exact on group and it would silently never match |
| `WorkloadPolicySpec` | `horizontalScaling` requires `targetRef.kind == Deployment` when a `targetRef` is set (the HPA reconciler only manages Deployments); `defaultRequests[r] <= defaultLimits[r]` for every resource in both (uses the CEL `quantity` library; values that are not valid quantities are skipped and left to the webhook) |

Size limits (D6, so rule cost stays bounded): `defaultRequests`/`defaultLimits` at most 16 entries with values of at most 64 characters (`QuantityString`); `mandatoryLabels` at most 32 entries with values of at most 63 (`LabelValueString`); `allowedRegistries` 64 items of at most 255; `excludedNamespaces` 256 items of at most 63; `allowedCapabilities` 64 items of at most 40.

Gotchas:
- controller-gen cannot bound the values of a `map[string]string` with `items:`; the bound goes on a named string type, which is why those maps are `map[string]QuantityString` / `map[string]LabelValueString` (convert with `string(v)`).
- Never write an empty-string literal (`''`) inside an `XValidation` rule: the generator turns it into a typographic quote and the CRD becomes invalid. Use `size(x) == 0`.
- Tests: `test/integration/cel_validation_test.go` (envtest, no webhooks). Each rule was verified by neutralising it and watching its specs fail.

## Cluster-wide Baselines (`ClusterSecurityBaseline`)

- Cluster-scoped CRD; its spec embeds `SecurityBaselineSpec` (all rules, `enforcementAction`, `excludedNamespaces`) and adds `namespaceSelector`. `targetRef` is rejected (CEL on the CRD + validating webhook); the inherited `selector` matches Pod labels.
- **Inheritance is additive:** `PodValidator.applicableBaselines` evaluates cluster baselines first, then the namespace's `SecurityBaseline`s. A namespaced baseline can add rules but never relax a cluster one (each baseline keeps its own `enforcementAction`).
- **Default exclusions** (never governed by a cluster baseline): `kube-system`, `kube-public`, `cert-manager`, the operator namespace (`POD_NAMESPACE` env, set via downward API in `config/manager/manager.yaml`) and the baseline's `excludedNamespaces`. The Pod webhook *configuration* also excludes them (plus `kube-node-lease`) by the immutable `kubernetes.io/metadata.name` label (PRE-02.3, see `config/webhook/patches/namespace-selector.yaml`), which is the real anti-deadlock guard with `failurePolicy: Fail`; `test/manifests` and the e2e availability specs protect it.
- **Fail closed:** if a `namespaceSelector` is set and the Namespace cannot be read, the validator returns 500 instead of skipping the baseline. An invalid `namespaceSelector` is rejected at write time; if one still reaches admission it is logged and skipped.
- RBAC: the manager needs `get/list/watch` on `namespaces` (kubebuilder marker in `clustersecuritybaseline_controller.go`).
- `PROJECT` was updated by hand for this kind (no `kubebuilder` CLI available); re-run `kubebuilder edit` checks if the CLI is installed later.

## Configuration

### Environment Variables (cmd/main.go flags)

| Flag | Default | Purpose |
|---|---|---|
| `--metrics-bind-address` | `0` | Metrics server (`:8080`, `:8443`, or `0` to disable) |
| `--health-probe-bind-address` | `:8081` | Liveness/readiness probes |
| `--leader-elect` | `false` | Enable leader election for HA |
| `--early-validation-mode` | `warn` | Early validation of Deployments/StatefulSets: `off` (webhook not registered), `warn` (warnings only), `enforce` (reject what Pod admission would reject) |
| `--enable-http2` | `false` | Disabled by default (security) |
| `--webhook-cert-path` | `` | TLS cert directory for webhooks |
| `--metrics-cert-path` | `` | TLS cert directory for metrics |

`ENABLE_WEBHOOKS=false` — disables all webhook registration (useful for local controller-only testing).

### Linting (`.golangci.yml`)

19 linters active: `copyloopvar`, `dupl`, `errcheck`, `ginkgolinter`, `goconst`, `gocyclo`, `govet`, `ineffassign`, `lll`, `modernize`, `misspell`, `nakedret`, `prealloc`, `revive`, `staticcheck`, `unconvert`, `unparam`, `unused`, `logcheck`.

Custom `logcheck` enforces Kubernetes-style structured logging.

`goconst` is excluded for `_test.go` (repeated fixture literals read better inline); it stays active for production code. golangci-lint must be a version whose `x/tools` can read the installed Go's export data — bump `GOLANGCI_LINT_VERSION` in `Makefile` and `.custom-gcl.yml` together when upgrading Go.

## Adding a New CRD

1. `kubebuilder create api --group core --version v1alpha1 --kind Foo`
2. Define spec/status in `api/v1alpha1/foo_types.go` with `+kubebuilder:` markers
3. `make manifests && make generate`
4. Implement `Reconcile()` in `internal/controller/foo_controller.go`
5. Add webhooks: `kubebuilder create webhook --group core --version v1alpha1 --kind Foo --defaulting --programmatic-validation`
6. Register controller + webhook in `cmd/main.go`
7. Add integration tests in `test/integration/foo_controller_test.go` (exported API; reuse `controllerSmokeBody` for the Reconcile smoke test) and, if needed, unit tests for unexported helpers in `internal/controller/foo_*_test.go`
8. Add sample CR in `config/samples/`

## kubebuilder CLI Reference

### Create API / Controller

```bash
# Own CRD
kubebuilder create api --group core --version v1alpha1 --kind Foo

# Controller for a core Kubernetes type (no new CRD)
kubebuilder create api --group apps --version v1 --kind Deployment \
  --controller=true --resource=false

# Controller for an external operator type (cert-manager, Argo CD, etc.)
kubebuilder create api \
  --group cert-manager --version v1 --kind Certificate \
  --controller=true --resource=false \
  --external-api-path=github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1 \
  --external-api-domain=io \
  --external-api-module=github.com/cert-manager/cert-manager

# Deploy-image plugin (scaffold a complete controller that manages a container image)
kubebuilder create api --group core --version v1alpha1 --kind MyApp \
  --image=<your-image> --plugins=deploy-image.go.kubebuilder.io/v1-alpha
```

The **deploy-image plugin** generates a full reference implementation: status conditions, finalizers, owner references, events, idempotent reconciliation.

### Create Webhooks

```bash
# Validation + defaulting for own CRD
kubebuilder create webhook --group core --version v1alpha1 --kind Foo \
  --defaulting --programmatic-validation

# Conversion webhook (multi-version, hub-and-spoke: v1 = hub, v2 = spoke)
kubebuilder create webhook --group core --version v1 --kind Foo \
  --conversion --spoke v2

# Webhook for external type
kubebuilder create webhook \
  --group cert-manager --version v1 --kind Issuer \
  --defaulting \
  --external-api-path=github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1 \
  --external-api-domain=io \
  --external-api-module=github.com/cert-manager/cert-manager
```

### Multi-group layout

When the project grows to multiple API groups, run:

```bash
kubebuilder edit --multigroup=true
```

Then reorganise manually:
1. Move APIs: `mkdir -p api/<group> && mv api/v1alpha1 api/<group>/`
2. Move controllers: `mkdir -p internal/controller/<group> && mv internal/controller/*.go internal/controller/<group>/`
3. Move webhooks: `mkdir -p internal/webhook/<group> && mv internal/webhook/v1alpha1 internal/webhook/<group>/`
4. Update all import paths
5. Fix `path` in `PROJECT` for each resource
6. Update test suite CRD paths (add one more `..` to relative paths)

## Controller Design Rules

- **Idempotent reconciliation** — safe to run multiple times with the same result
- **Re-fetch before update** — `r.Get(ctx, req.NamespacedName, obj)` before `r.Update` to avoid conflicts
- **Watch owned resources** — use `.Owns()` or `.Watches()`, not just `RequeueAfter`
- **Owner references** — call `controllerutil.SetControllerReference` to enable automatic GC
- **Finalizers** — required when cleaning up external resources (buckets, VMs, DNS entries)
- **Structured logging** — `log := logf.FromContext(ctx); log.Info("Msg", "key", val)`

### Kubernetes logging style

```go
// Capital letter, no trailing period, active voice, past tense for errors
log.Info("Starting reconciliation")
log.Info("Created HPA", "name", hpa.Name, "namespace", hpa.Namespace)
log.Error(err, "Failed to update status", "policy", req.NamespacedName)
```

Reference: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md

## Distribution

### YAML bundle (Kustomize)

```bash
make build-installer IMG=<registry>/<project>:<tag>
# Generates dist/install.yaml — commit this for easy single-command installs:
kubectl apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/dist/install.yaml
```

### Helm chart

```bash
kubebuilder edit --plugins=helm/v2-alpha              # Generates dist/chart/
kubebuilder edit --plugins=helm/v2-alpha --output-dir=charts

make helm-deploy IMG=<registry>/<project>:<tag>       # Deploy via Helm
make helm-status / helm-uninstall / helm-rollback
```

If webhooks are added after initial chart generation: backup `values.yaml`, re-run `kubebuilder edit --plugins=helm/v2-alpha --force`, restore customisations.

## Docs, Architecture Governance & Agile Backlog (`../0.docs/`)

- `../0.docs/README.md` — Governance hub, agile taxonomy (Epics, Features, Enablers, HUs, Tasks), and DoD rules
- `../0.docs/principles/PRIN-001-principios-de-arquitectura-y-cumplimiento.md` — Kube-native principles, Go design patterns & ISO 9001/27001 compliance
- `../0.docs/reviews/REV-001-initial-maturity-and-iso-gap-analysis.md` — ISO 9001, ISO 27001 & Service Mesh gap analysis
- `../0.docs/plans/PLAN-001-production-maturity-and-mesh-alignment.md` — Master architecture & phased evolution plan
- `../0.docs/backlog/BACKLOG.md` — Master Scrum backlog & status tracker (`EPIC-01` through `EPIC-04`)
- `../0.docs/bitacora/README.md` — Session changelog & handoff notes
- `docs/adrs/0002-webhook-failure-policy-fail.md` — Why webhooks use `failurePolicy: fail`
- `docs/blog/01-designing-platform-contracts-with-crds.md` — Design philosophy

## References

- **Kubebuilder Book:** https://book.kubebuilder.io
- **controller-runtime FAQ:** https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md
- **Good Practices:** https://book.kubebuilder.io/reference/good-practices.html
- **API Conventions:** https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
- **Markers Reference:** https://book.kubebuilder.io/reference/markers.html
- **Logging Guidelines:** https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md
