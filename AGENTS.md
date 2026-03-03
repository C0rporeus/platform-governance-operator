# CLAUDE.md

This file provides guidance to Claude Code when working with the `platform-governance-operator` codebase.

## Overview

A **Kubernetes Operator** that enforces platform governance through three Custom Resource Definitions (CRDs) and five admission webhooks. It acts as a security and standards enforcement layer over any Kubernetes cluster.

- **Domain:** `platform.f3nr1r.io`
- **Group:** `core.platform.f3nr1r.io`
- **API Version:** `v1alpha1`
- **Module:** `github.com/f3nr1r/platform-governance-operator`
- **Go:** 1.25.0 | **controller-runtime:** v0.23.1 | **k8s.io/api:** v0.35.0

## Repository Structure

```
cmd/main.go                         Manager entry point (registers controllers + webhooks)
api/v1alpha1/                       CRD type definitions (kubebuilder markers live here)
  securitybaseline_types.go
  workloadpolicy_types.go
  telemetryprofile_types.go
  zz_generated.deepcopy.go          AUTO-GENERATED — never edit
internal/controller/                Reconciliation logic (one file per CRD)
  securitybaseline_controller.go
  workloadpolicy_controller.go      Includes HPA lifecycle management
  telemetryprofile_controller.go
  reconcile_status_helper.go        Shared status condition helpers
  reconcile_status_helper_test.go   Unit tests for unexported status helper
  workloadpolicy_hpa_test.go        Unit tests for unexported HPA helpers
internal/webhook/
  v1alpha1/                         CRD defaulting/validating webhooks
  core/                             Core API (Pod) webhooks
    pod_webhook.go                  Validating webhook
    pod_mutating_webhook.go         Mutating webhook
config/
  crd/bases/                        AUTO-GENERATED — never edit
  rbac/role.yaml                    AUTO-GENERATED — never edit
  webhook/manifests.yaml            AUTO-GENERATED — never edit
  samples/                          Example CRs — edit freely
  default/                          Kustomization entry point
docs/adrs/                          Architecture Decision Records
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
| Integration | `test/integration/` | Ginkgo + envtest | Reconcile() smoke + HPA lifecycle (public API only) |
| E2E | `test/e2e/` | Ginkgo + Kind cluster | Full deployment (build tag `//go:build e2e`) |

**Rule:** `internal/controller/` only keeps `_test.go` files that test **unexported** symbols. Tests for exported behaviour go in `test/integration/`.

### Linting

```bash
make lint          # golangci-lint (19 linters)
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
- `internal/webhook/v1alpha1/webhook_suite_test.go` — webhook suite setup
- CRDs loaded from `config/crd/bases/` by envtest (requires `make manifests` first)
- Black-box tests import the controller package: `controller.WorkloadPolicyReconciler{...}`

## Configuration

### Environment Variables (cmd/main.go flags)

| Flag | Default | Purpose |
|---|---|---|
| `--metrics-bind-address` | `0` | Metrics server (`:8080`, `:8443`, or `0` to disable) |
| `--health-probe-bind-address` | `:8081` | Liveness/readiness probes |
| `--leader-elect` | `false` | Enable leader election for HA |
| `--enable-http2` | `false` | Disabled by default (security) |
| `--webhook-cert-path` | `` | TLS cert directory for webhooks |
| `--metrics-cert-path` | `` | TLS cert directory for metrics |

`ENABLE_WEBHOOKS=false` — disables all webhook registration (useful for local controller-only testing).

### Linting (`.golangci.yml`)

19 linters active: `copyloopvar`, `dupl`, `errcheck`, `ginkgolinter`, `goconst`, `gocyclo`, `govet`, `ineffassign`, `lll`, `modernize`, `misspell`, `nakedret`, `prealloc`, `revive`, `staticcheck`, `unconvert`, `unparam`, `unused`, `logcheck`.

Custom `logcheck` enforces Kubernetes-style structured logging.

## Adding a New CRD

1. `kubebuilder create api --group core --version v1alpha1 --kind Foo`
2. Define spec/status in `api/v1alpha1/foo_types.go` with `+kubebuilder:` markers
3. `make manifests && make generate`
4. Implement `Reconcile()` in `internal/controller/foo_controller.go`
5. Add webhooks: `kubebuilder create webhook --group core --version v1alpha1 --kind Foo --defaulting --programmatic-validation`
6. Register controller + webhook in `cmd/main.go`
7. Add integration tests in `test/integration/foo_controller_test.go` (exported API) and, if needed, unit tests for unexported helpers in `internal/controller/foo_*_test.go`
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

## Docs & ADRs

- `docs/adrs/0002-webhook-failure-policy-fail.md` — Why webhooks use `failurePolicy: fail`
- `docs/blog/01-designing-platform-contracts-with-crds.md` — Design philosophy

## References

- **Kubebuilder Book:** https://book.kubebuilder.io
- **controller-runtime FAQ:** https://github.com/kubernetes-sigs/controller-runtime/blob/main/FAQ.md
- **Good Practices:** https://book.kubebuilder.io/reference/good-practices.html
- **API Conventions:** https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
- **Markers Reference:** https://book.kubebuilder.io/reference/markers.html
- **Logging Guidelines:** https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md
