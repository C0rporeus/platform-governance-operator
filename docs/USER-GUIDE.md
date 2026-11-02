# Platform Governance Operator — Technical User Guide

This guide is for platform engineers who install the operator, and for application teams whose workloads it governs. It covers installation, the policy model, a safe adoption path, ready-to-use recipes, operations and troubleshooting.

For the reasons behind each design choice see [`GOOD-PRACTICES-EVIDENCE.md`](./GOOD-PRACTICES-EVIDENCE.md) and the [ADRs](./adrs/).

---

## 1. What the operator does

The operator turns platform standards into Kubernetes resources and enforces them when Pods are admitted:

| Resource | Scope | Effect at Pod creation |
| :--- | :--- | :--- |
| `ClusterSecurityBaseline` | Cluster | Security rules for every non-excluded namespace. Namespaced baselines can add to it, never relax it. |
| `SecurityBaseline` | Namespace | Security rules for the namespace (or for selected workloads in it). |
| `WorkloadPolicy` | Namespace | Fills in missing resource requests/limits and mandatory labels; optionally manages an HPA per Deployment. |
| `TelemetryProfile` | Namespace | Injects OpenTelemetry environment variables into containers. |

Each security rule is `Enforce` (reject), `Warn` (admit and show a warning to the client) or `Audit` (admit silently and record an event). Every violation names the rule, an ISO/IEC 27001 control and the policy that raised it:

```text
Error from server (Forbidden): admission webhook "vpod.kb.io" denied the request:
Pod violates SecurityBaseline: [RunAsNonRoot / ISO/IEC 27001 A.8.2] app container 'app' must run as non-root (policy: strict)
```

**It is not** a replacement for [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/); use both. PSA gives you the upstream profiles per namespace. The operator adds targeting by workload, cluster-wide inheritance, supply-chain rules, gradual enforcement and defaults injection.

---

## 2. Requirements

| Requirement | Notes |
| :--- | :--- |
| Kubernetes **1.30+** | Minimum supported version. CI and envtest use current releases. |
| [cert-manager](https://cert-manager.io/) | Issues the webhook serving certificates and injects the CA bundle. Install it first. |
| 2+ schedulable nodes | Recommended. The operator runs 2 replicas spread across nodes, and admission depends on it (see §8). |
| A registry for the operator image | Any registry your nodes can pull from. |

---

## 3. Installation

### 3.1 Build and publish the image

```bash
export IMG=<registry>/platform-governance-operator:<version>
make docker-build docker-push IMG=$IMG
```

### 3.2 Option A — single install manifest (recommended for GitOps)

```bash
make build-installer IMG=$IMG     # writes dist/install.yaml (CRDs, RBAC, Deployment, webhooks, Certificate)
kubectl apply -f dist/install.yaml
```

Commit `dist/install.yaml` (or publish it as a release asset) and let your GitOps tool apply it.

### 3.3 Option B — straight from the repository

```bash
make deploy IMG=$IMG              # kustomize build config/default | kubectl apply -f -
```

### 3.4 Verify

```bash
kubectl -n platform-governance-operator-system rollout status deploy/platform-governance-operator-controller-manager
kubectl get crd | grep core.platform.f3nr1r.io
# A server-side dry run goes through the webhooks without creating anything:
kubectl run probe --image=registry.k8s.io/pause:3.10 --restart=Never --dry-run=server
```

### 3.5 GitOps ordering (Argo CD / Flux)

Apply in this order, and wait for each step to be healthy:

1. **cert-manager.**
2. **The operator** (`dist/install.yaml`). Wait until the Deployment is available and the webhook configurations have a `caBundle`.
3. **Policies** (`ClusterSecurityBaseline`, `SecurityBaseline`, …). Their CRDs and validating webhooks must already exist.

With Argo CD, use sync waves (`argocd.argoproj.io/sync-wave: "0"`, `"1"`, `"2"`). With Flux, use separate `Kustomization` objects linked by `dependsOn`.

### 3.6 Changing the install namespace

The default namespace is `platform-governance-operator-system`. If you change `namespace:` in `config/default/kustomization.yaml`, change the same name in `config/webhook/patches/namespace-selector.yaml`. If that patch still names the old namespace, the operator's own Pods are no longer excluded from its webhooks and it can block itself. `go test ./test/manifests/` fails until both match.

---

## 4. The policy model

### 4.1 Targeting

Every policy can narrow what it applies to:

```yaml
spec:
  targetRef:            # one specific workload
    group: apps
    kind: Deployment
    name: payments-api
  # or
  selector:             # workloads whose Pod labels match
    matchLabels:
      tier: data
  # neither: the whole namespace
```

`targetRef` matches a Pod through its owners. A Pod created by a Deployment is matched by `kind: Deployment`, a Pod created by a StatefulSet by `kind: StatefulSet`, and so on.

**Precedence**, from most to least specific: `targetRef` → `selector` → namespace-wide → cluster. Among policies at the same level, the higher `priority` wins, then namespace and name decide.

- **Security baselines are additive.** Every applicable baseline is evaluated. A workload must satisfy all of them, and each keeps its own enforcement action.
- **`WorkloadPolicy` and `TelemetryProfile` fill gaps.** Policies are applied from most to least specific. Each one only sets fields that are still empty, so the most specific policy wins per field. Values the developer wrote are never overwritten.

### 4.2 Cluster baselines and exclusions

A `ClusterSecurityBaseline` applies to every namespace except:

- `kube-system`, `kube-public` and `cert-manager`;
- the operator's own namespace;
- the namespaces in its `excludedNamespaces`;
- with a `namespaceSelector`, the namespaces whose labels do not match.

The Pod webhooks additionally never receive requests from `kube-system`, `kube-public`, `kube-node-lease`, `cert-manager` or the operator namespace.

> **Exempt by name, not by label.** Anyone who can `patch` a Namespace can change its labels and leave a `namespaceSelector`. Use `excludedNamespaces`, or select on the immutable `kubernetes.io/metadata.name` label, and restrict `patch` on namespaces with RBAC.

### 4.3 What is evaluated, and when

- **Pod creation:** every rule of every applicable baseline.
- **Pod update:** updates that only change metadata (except seccomp/AppArmor annotations), `activeDeadlineSeconds` or `tolerations` are allowed without evaluation, matching Pod Security Admission. You can still relabel a running Pod that predates a stricter baseline. Any other change, such as a new image, is evaluated in full.
- **Init containers** are evaluated like app containers.
- **Windows Pods** (`spec.os.name: windows`) skip the Linux-only rules (privilege escalation, seccomp, capabilities, read-only root filesystem). `runAsNonRoot` still applies.
- **Mirror Pods** of static Pods are skipped only when the request comes from the node.
- **Server-side dry runs** are evaluated but record no events.

---

## 5. Adoption path: Audit → Warn → Enforce

Never introduce a new rule straight into `Enforce` on a busy cluster.

1. **Audit.** Create the baseline with `enforcementAction: Audit`. Nothing is blocked. Collect violations:
   ```bash
   kubectl get events -A --field-selector reason=PolicyViolationAudit
   ```
2. **Fix workloads** using the table in §7. Track progress by team or namespace.
3. **Warn.** Switch to `Warn`. Developers see warnings in `kubectl apply` and in CI logs, but nothing is blocked:
   ```text
   Warning: [ReadOnlyRootFilesystem / ISO/IEC 27001 A.8.9] app container 'web' must have read-only root filesystem (policy: corporate)
   ```
4. **Enforce.** Switch to `Enforce` once the audit events stop. Do it namespace by namespace, or use a `selector` per team.

**Ratchet with layers.** Keep the corporate `ClusterSecurityBaseline` at the level everybody meets. Raise stricter rules through namespaced `SecurityBaseline`s for teams that are ready.

---

## 6. Recipes

### 6.1 Corporate baseline close to PSS *restricted*

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: ClusterSecurityBaseline
metadata:
  name: corporate
spec:
  enforcementAction: Warn          # move to Enforce after §5
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  disallowPrivileged: true
  disallowPrivilegeEscalation: true
  requireDropAllCapabilities: true
  allowedCapabilities: ["NET_BIND_SERVICE"]
  requireSeccompProfile: true
  disallowHostNamespaces: true
  disallowHostPathVolumes: true
  excludedNamespaces:
  - monitoring                     # e.g. node exporters that need host access
```

### 6.2 Supply chain: trusted registries, no mutable tags

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: SecurityBaseline
metadata:
  name: supply-chain
  namespace: payments
spec:
  enforcementAction: Enforce
  runAsNonRoot: false              # covered by the corporate baseline
  readOnlyRootFilesystem: false
  allowedRegistries:
  - 123456789012.dkr.ecr.us-east-1.amazonaws.com/
  - ghcr.io/my-org/
  disallowLatestTag: true
  requireImageDigest: true         # image@sha256:...
```

`runAsNonRoot` and `readOnlyRootFilesystem` default to `true`. Set them to `false` explicitly in a baseline that should check only other rules.

### 6.3 Resource defaults and mandatory labels for a team

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: WorkloadPolicy
metadata:
  name: team-defaults
  namespace: payments
spec:
  defaultRequests: { cpu: 100m, memory: 128Mi }
  defaultLimits:   { memory: 256Mi }
  mandatoryLabels:
    cost-center: cc-1234
    owner: team-payments
```

Containers that already declare a request, limit or label keep their value.

### 6.4 An HPA for every Deployment, with opt-out

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: WorkloadPolicy
metadata:
  name: autoscaling
  namespace: payments
spec:
  horizontalScaling:
    enabledByDefault: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
```

The operator creates `<deployment>-pgo-hpa`, labelled `core.platform.f3nr1r.io/managed-hpa: "true"`. HPAs you created yourself are never touched.

- To opt a Deployment out, annotate it with `core.platform.f3nr1r.io/hpa-enabled: "false"`.
- To opt in when `enabledByDefault` is `false`, use `"true"`.
- CPU-based HPAs need CPU requests on the containers (§6.3) and metrics-server in the cluster.

### 6.5 Tracing for every Pod

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: TelemetryProfile
metadata:
  name: otel
  namespace: payments
spec:
  tracingEndpoint: http://otel-collector.observability:4317
  injectEnvVars: true
  samplingRate: "0.25"
```

This injects `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_TRACES_SAMPLER_ARG` into app containers (not init containers), unless the container already defines them.

### 6.6 One stricter rule for one workload

```yaml
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: SecurityBaseline
metadata:
  name: payments-api-digest
  namespace: payments
spec:
  targetRef: { group: apps, kind: Deployment, name: payments-api }
  enforcementAction: Enforce
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  requireImageDigest: true
```

---

## 7. Making workloads compliant

A Pod template that satisfies every rule:

```yaml
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        seccompProfile: { type: RuntimeDefault }
      containers:
      - name: app
        image: ghcr.io/my-org/app@sha256:<digest>
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities: { drop: ["ALL"] }
        volumeMounts:
        - { name: tmp, mountPath: /tmp }   # writable scratch space with a read-only root fs
      volumes:
      - { name: tmp, emptyDir: {} }
```

| Rule in the message | Fix |
| :--- | :--- |
| `RunAsNonRoot` | Set `runAsNonRoot: true` (Pod or container) and use an image whose `USER` is a non-zero UID, or set `runAsUser`. |
| `ReadOnlyRootFilesystem` | Set `readOnlyRootFilesystem: true`; mount `emptyDir` volumes where the app writes (`/tmp`, caches). |
| `DisallowPrivileged` | Remove `privileged: true`. |
| `DisallowPrivilegeEscalation` | Set `allowPrivilegeEscalation: false`. |
| `DropAllCapabilities` | Set `capabilities.drop: ["ALL"]`; add back only the capabilities the baseline allows. |
| `SeccompProfile` | Set `seccompProfile.type: RuntimeDefault` (or `Localhost`). |
| `DisallowHostNamespaces` | Remove `hostNetwork`, `hostPID` and `hostIPC`. |
| `DisallowHostPath` | Replace `hostPath` volumes with `emptyDir`, a PVC, a ConfigMap or a projected volume. |
| `SupplyChain` | Use an allowed registry, an explicit tag other than `latest`, or `@sha256:` when digests are required. |

**Catch violations before deploying.** A server-side dry run runs the same admission chain without creating anything. It works in CI against a staging cluster:

```bash
kubectl apply --dry-run=server -f k8s/
```

**Early feedback on Deployments, StatefulSets, DaemonSets, Jobs and CronJobs.** When you create a workload or change its Pod template, the operator evaluates the template with the same rules Pod admission will apply, and reports the result on `kubectl apply`:

```text
Warning: [early] [RunAsNonRoot / ISO/IEC 27001 A.8.2] app container 'app' must run as non-root (policy: corporate)
deployment.apps/web configured
```

- The default mode is `warn`: you see the warnings and the workload is still applied. Platform teams can switch to `enforce` (`--early-validation-mode=enforce`) once the warnings are gone; then the apply is rejected for the violations Pod admission would reject. `off` disables it.
- Changing only replicas, labels or annotations of the workload is not evaluated; a change to the Pod template (an image, or `kubectl rollout restart`) is.
- It is feedback, not the security boundary: if the operator is unavailable the apply goes through, and every Pod is still validated when it is created.
- It evaluates the Pod template, not the final Pod: it cannot see what the operator or other webhooks (for example a service mesh injecting sidecars) add to Pods later, or per-Pod names, so a policy that depends on those is only enforced at Pod admission. Closing these gaps is planned (see the project backlog, `HU-02.4`).

If a rollout still makes no progress, check `kubectl describe rs` and the namespace events: Pod admission is the final check.

---

## 8. Operating the operator

### 8.1 Availability model

- The Pod webhooks use `failurePolicy: Fail` ([ADR-0002](./adrs/0002-webhook-failure-policy-fail.md)): if no replica answers, **Pod creation is blocked** in governed namespaces. That is deliberate, because a governance layer that silently admits everything gives false assurance.
- To make that safe, the operator runs **2 replicas** with anti-affinity, topology spread and a `PodDisruptionBudget` (`minAvailable: 1`). Every webhook has `timeoutSeconds: 5`.
- Controllers use leader election (one active reconciler). Both replicas serve webhooks.
- The operator namespace and `cert-manager` are excluded from the Pod webhooks, so the operator and its certificates can always restart.

All of the above is checked by the end-to-end suite on a 3-node cluster: a replica is killed while admission keeps enforcing, and the operator and cert-manager restart under a baseline their own Pods would violate.

### 8.2 Who may change policy (RBAC)

| Role | Suggested permissions |
| :--- | :--- |
| Platform team | Write `clustersecuritybaselines`. Write the namespaced kinds everywhere. |
| Team leads | Write `securitybaselines`, `workloadpolicies` and `telemetryprofiles` in their namespaces (the generated `*-editor-role` ClusterRoles can be bound per namespace). |
| Developers | Read only (`*-viewer-role`). |

Restrict `patch` on `namespaces` (see §4.2), and keep `excludedNamespaces` changes behind review.

### 8.3 Observability

```bash
# Decisions, per namespace
kubectl get events -n <ns> --field-selector reason=PodDenied
kubectl get events -n <ns> --field-selector reason=PolicyViolationWarn
kubectl get events -n <ns> --field-selector reason=PolicyViolationAudit
kubectl get events -n <ns> --field-selector reason=PodMutated

# Operator logs (structured JSON)
kubectl -n platform-governance-operator-system logs deploy/platform-governance-operator-controller-manager --all-containers

# Policy health
kubectl get securitybaselines,workloadpolicies,telemetryprofiles -A
kubectl get clustersecuritybaselines
```

Controller-runtime metrics are served over HTTPS on port 8443 behind authentication. Grant the `platform-governance-operator-metrics-reader` ClusterRole to your Prometheus service account. Governance-specific metrics and compliance status are planned (EPIC-03).

### 8.4 Upgrades

Apply the new `dist/install.yaml`. The rolling update keeps one replica serving (PDB and `maxUnavailable`). CRD changes in `v1alpha1` are additive.

### 8.5 Break-glass

If the operator cannot run and Pod creation is blocked, a cluster admin can remove the Pod webhooks. This **suspends all governance** until the operator is redeployed:

```bash
kubectl delete mutatingwebhookconfiguration platform-governance-operator-mutating-webhook-configuration
kubectl delete validatingwebhookconfiguration platform-governance-operator-validating-webhook-configuration
# fix the cause, then re-apply dist/install.yaml to restore them
```

Record the action. It is an exception to your access control process.

### 8.6 Uninstall

Delete the policies first, then the operator, then cert-manager if nothing else uses it:

```bash
kubectl delete clustersecuritybaselines --all
kubectl delete securitybaselines,workloadpolicies,telemetryprofiles -A --all
make undeploy        # or: kubectl delete -f dist/install.yaml
```

Deleting a `WorkloadPolicy` does not delete the HPAs it created (there is no finalizer yet). Remove them explicitly:

```bash
kubectl delete hpa -A -l core.platform.f3nr1r.io/managed-hpa=true
```

---

## 9. Troubleshooting

| Symptom | Likely cause | What to do |
| :--- | :--- | :--- |
| `failed calling webhook "vpod.kb.io" … connection refused` | No operator replica is ready, or its Service has no endpoints. | `kubectl -n platform-governance-operator-system get pods,endpointslices`; check the logs. See §8.5 if it is urgent. |
| `x509: certificate signed by unknown authority` | cert-manager has not injected the CA yet, or it is not installed. | `kubectl get certificate -n platform-governance-operator-system`; check that the `caBundle` is set on the webhook configurations. |
| A Deployment rollout makes no progress | Its Pods are denied. | `kubectl describe rs -n <ns>` and `kubectl get events -n <ns> --field-selector reason=PodDenied`. |
| A new policy has no effect for a few seconds | The webhook reads policies from an informer cache. | Retry. Policies propagate within seconds. |
| A `targetRef` to a Deployment does not match | The Pod's ReplicaSet name does not end with its `pod-template-hash` label (a custom controller). | Use a `selector` on Pod labels instead. |
| A policy is rejected at creation | Invalid `selector`; a `targetRef` without `kind`/`name`, or whose `kind` does not belong to its `group` (`Deployment` needs `group: apps`; `CronJob` is not accepted because it owns Jobs, not Pods); `maxReplicas` below `minReplicas`; `horizontalScaling` on a `targetRef` that is not a Deployment; `defaultRequests` above `defaultLimits`; a list or map over its size limit; `ClusterSecurityBaseline` does not accept `targetRef`. | Read the validation message; fix the field it names. These checks run on `kubectl apply` (Kubernetes 1.30+). |
| No HPA is created | `horizontalScaling` is missing, `enabledByDefault: false` without the opt-in annotation, or a more specific policy without `horizontalScaling` governs the Deployment. | `kubectl get workloadpolicy -n <ns> -o yaml`; check the Deployment annotations. |

---

## 10. Trying it locally (Docker Desktop)

The end-to-end suite creates a disposable 3-node [Kind](https://kind.sigs.k8s.io/) cluster in Docker, installs cert-manager and the operator, runs the scenarios in this guide, and deletes the cluster:

```bash
make test-e2e                  # Docker Desktop must be running
make test-e2e E2E_KEEP=true    # keep the cluster to experiment
export KUBECONFIG=$PWD/bin/e2e-kubeconfig
kubectl apply -f config/samples/
make cleanup-test-e2e
```

The suite uses its own kubeconfig (`bin/e2e-kubeconfig`) and refuses to run against any other context. Your `~/.kube/config` and its current context are never touched. Set `KIND_NODE_IMAGE=kindest/node:v1.30.x` to test the minimum supported Kubernetes version.

---

## 11. Known limitations

- Only Pods are enforced. `Deployment`, `StatefulSet`, `DaemonSet`, `Job` and `CronJob` get early feedback (warnings by default); Pods created by other controllers are validated when they are created.
- Workloads that were already running when a policy was created are not re-evaluated; the early check only looks at template changes. A continuous audit of existing workloads is planned (`HU-02.5`).
- Warnings are informational. To make them a change-control gate, fail your pipeline when `kubectl apply --dry-run=server` prints `Warning: [early]` (a ready-made script is planned, `TASK-02.6.9`).
- Pods created by controllers outside Kubernetes (for example Argo Rollouts) get no early feedback; Pod admission validates them.
- Ephemeral containers (`kubectl debug`) are added through a subresource that the webhooks do not intercept.
- `mandatoryLabels` are added to Pods, not to their owning workloads.
- ISO/IEC 27001 control references in messages are an interpretation pending confirmation by your ISMS owner. Treat them as traceability hints, not as audit evidence.
