# Good Practices Evidence and Design Basis

| | |
| :--- | :--- |
| **Document ID** | GPE-001 |
| **Version** | 1.2 |
| **Date** | 2026-10-28 |
| **Subject** | Platform Governance Operator: a Kubernetes governance solution |
| **Parent document** | [Project README](../README.md#documentation) |
| **Review triggers** | A source in the register changes version; a design decision is added, changed or withdrawn; at least once per minor release |

---

## 1. Purpose and scope

This document is the documented evidence that the design of the Platform Governance Operator rests on researched, attributable good practices rather than on preference. It records:

1. **How** the practices were gathered: the method, the selection criteria and the verification protocol ([section 2](#2-method)).
2. **From what**: every source consulted, with its publisher, version and the extent to which it was read ([section 3](#3-source-register)).
3. **What was learned**: the practices extracted, each tied to a section of its source ([section 4](#4-practice-catalogue)).
4. **What was decided**: the design decisions the practices informed, the alternatives considered and the deviations accepted ([sections 5 to 7](#5-design-decisions)).
5. **How it can be checked**: traceability from each decision to the artefact that realises it and the test that verifies it ([section 6](#6-traceability-matrix)).

**What this document is not.** It is not a development guide (see `AGENTS.md`), not a certification or a claim of compliance with any framework, and not a progress report. Point-in-time conformance findings are kept apart in [Annex A](#annex-a-conformance-snapshot-point-in-time) so that the body of this document stays stable between reviews.

**Intended readers:** architects and security reviewers evaluating the design, auditors tracing a control to a mechanism, and maintainers deciding whether a change contradicts an established practice.

---

## 2. Method

### 2.1 Domains investigated

Eight domains cover what a Kubernetes governance operator touches: **A** admission control; **B** pod security; **C** RBAC; **D** networking; **E** images and supply chain; **F** API and policy-model design; **G** reliability, operations and observability; **H** security governance frameworks.

### 2.2 Source tiers and selection criteria

| Tier | Kind of source | Used for |
| :---: | :--- | :--- |
| **1** | Documentation by the maintainers of the technology, and guidance from foundations and standards bodies (Kubernetes and Gateway API under the CNCF/Linux Foundation, CNCF TAG Security, OpenSSF, SLSA, NIST) | Authority on how Kubernetes behaves and on cluster-security practice |
| **2** | Cloud-provider architecture frameworks (Google Cloud, AWS) | Corroboration, and reliability / operations principles. Not authoritative on Kubernetes behaviour |
| **3** | ISO/IEC 27001:2022 Annex A | Control-objective traceability only. The standard is paywalled: titles come from a secondary summary |

A source is admitted when it (a) is authored by the maintainers or a standards body, (b) is versioned or dated, (c) addresses at least one domain above and (d) is actionable at the level of cluster governance or admission control. Vendor marketing, blogs, tool documentation not tied to a standard, and unattributed summaries are excluded.

### 2.3 Extraction and verification protocol

Every page was retrieved on **2026-10-28**. A statement enters the catalogue only if it can be attributed to a named section of a retrieved page. Each source carries one verification level:

| Level | Meaning |
| :---: | :--- |
| **L** | Lossless. The whole page was downloaded, hashed (SHA-256 of the raw bytes), converted to text with its section structure and read in full from that text. The hash and the review status are in the [source ledger](evidence/source-ledger.md). |
| **V** | Verified through an automated page reader that summarises and may truncate. Sections relied upon were retrieved, but the reader's omissions cannot be ruled out. Being upgraded to **L** (backlog `EPIC-05`). |
| **P** | Partial. The page was truncated at retrieval; only the retrieved sections are relied upon and the unread portion is listed in the register. |
| **S** | Secondary. Titles come from a third-party summary of a paywalled normative text. |
| **C** | Catalogued. The source is identified for completeness; no content was extracted and nothing is relied upon. |

**Why L exists.** The first version of this document relied on a page reader that truncated long pages and summarised them. Downloading the pages showed that their real content is much smaller than the reader suggested (for example, S-01 is 26,659 characters of text) and that the reader had skipped whole sections. Reading S-01 and S-06 losslessly added eleven practices and exposed five gaps in the solution (see sections 4 and Annex A). Sources not yet upgraded to **L** are listed with their pending sections in the ledger; until then their **V** level carries that caveat.

**Material recalled from memory is never used as evidence.** Where a practice was first recalled and then checked, only the checked text is cited.

*Corrections made during compilation, kept for transparency:*
- The AWS Well-Architected Security design principles were first returned without page content; they were re-retrieved from a second URL before being recorded as **V**.
- The AWS Operational Excellence list differed from the recalled one (it now has seven principles, led by "Organize teams around business outcomes"); the retrieved list is the one recorded.
- The SLSA v1.0 page is marked *retired* by its publisher. It was re-read against v1.2 (current): the Build track levels L0 to L3 are unchanged, and v1.2 adds a Source track (L1 to L4). BP-SC-07 and BP-SC-08 reflect this.
- ISO/IEC 27001:2022 could not be retrieved: `iso.org` answers HTTP 403 to automated clients and no browser session was available. S-22 stays a secondary summary and the question of what is publicly readable is open (`ENAB-05.6`).
- An example in the repository (`config/samples/core_v1alpha1_clustersecuritybaseline.yaml`) exempted namespaces by a mutable label. Reading the RBAC good-practices page showed that this lets anyone able to patch a Namespace opt out; the example was corrected (see BP-RBAC-06 and DD-06).

### 2.4 Traceability chain

```text
Source (S-nn)  →  Practice (BP-dom-nn)  →  Design decision (DD-nn)  →  Realisation (artefact)  →  Verification (test)
                                                   └→  Deviation (DEV-nn), where the solution departs from a source
```

### 2.5 Limitations

- Pages were retrieved and summarised by an automated tool. Wording is quoted only in short fragments; the original page prevails in any dispute.
- Several pages exceed the retrieval limit; the unread portions are stated in the register and nothing is inferred from them.
- Documentation changes. The register records the access date; a source marked with a version (for example "Kubernetes 1.37" pages) reflects that version.
- Decisions that predate this research (for example ADR-0002, 2026-02-26) were not derived from it. For those, the sources **corroborate** the decision; the *Origin* column in [section 5](#5-design-decisions) says which is which. This document does not claim a provenance the history does not support.

---

## 3. Source register

| ID | Source | Publisher | Version / date | Tier | Level | Sections relied upon | Unread or caveat |
| :--- | :--- | :--- | :--- | :---: | :---: | :--- | :--- |
| S-01 | [Admission Webhook Good Practices](https://kubernetes.io/docs/concepts/cluster-administration/admission-webhooks-good-practices/) | Kubernetes | accessed 2026-10-28 | 1 | **L** | Whole page (41 sections) | none |
| S-02 | [Validating Admission Policy](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/) | Kubernetes | GA since 1.30 | 1 | **P** | Stability; `paramKind`; `validationActions`; matching `apps/v1` resources | CEL cost-limit and type-checking sections not read |
| S-03 | [RBAC Good Practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/) | Kubernetes | accessed 2026-10-28 | 1 | **V** | General good practice; token distribution; hardening; periodic review; privilege-escalation risks (including node proxy, `escalate`, `bind`, `impersonate`, CSRs, token request, webhook control, namespace modification); denial of service | none |
| S-04 | [Security Checklist](https://kubernetes.io/docs/concepts/security/security-checklist/) | Kubernetes | accessed 2026-10-28 | 1 | **V** | Authentication and authorisation; Network security; Pod security; Secrets; Images; Admission controllers; Logs and auditing; Pod placement; final notes | none |
| S-05 | [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) | Kubernetes | accessed 2026-10-28 | 1 | **V** | Profiles; Baseline controls; Baseline sysctls; Restricted controls | none |
| S-06 | [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) | Kubernetes | stable since 1.25 | 1 | **L** | Whole page, including the update exemptions | none |
| S-07 | [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/) | Kubernetes | pages for 1.37 | 1 | **P** | Prerequisites; pod isolation; selectors; default policies; limitations; port ranges; namespace-by-name label | Pod-lifecycle and trailing sections not read |
| S-08 | [Custom Resource Definitions: validation rules, ratcheting, cost](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/) | Kubernetes | rules stable 1.29; ratcheting stable 1.33 | 1 | **P** | Validation rules; transition rules; `optionalOldSelf`; validation ratcheting; resource use by validation functions | First ~52,000 and last ~17,700 characters not read |
| S-09 | [Common Expression Language in Kubernetes](https://kubernetes.io/docs/reference/using-api/cel/) | Kubernetes | accessed 2026-10-28 | 1 | **P** | Where CEL is used; library availability table; runtime cost budget and estimation; escaping | Library-specific sections after the list library not read |
| S-10 | [GEP-713: Metaresources and Policy Attachment](https://gateway-api.sigs.k8s.io/geps/gep-713/) | Kubernetes SIG-Network (Gateway API) | accessed 2026-10-28 | 1 | **P** | Attachment classes; `targetRefs` fields; hierarchy and precedence; merge strategies; status conditions | Last ~5,000 characters |
| S-11 | [Cloud Native Security Whitepaper v2.0](https://github.com/cncf/tag-security/blob/main/community/resources/security-whitepaper/v2/cloud-native-security-whitepaper.md) | CNCF TAG Security | v2.0, reviewed 2022-05-16 | 1 | **P** | Lifecycle phases (Develop, Distribute, Deploy, Runtime); admission control and policy enforcement; policy as code; secure defaults; least privilege; defence in depth | Final ~26,700 characters (GitOps and supply-chain sections) |
| S-12 | [OpenSSF Scorecard checks](https://github.com/ossf/scorecard/blob/main/docs/checks.md) | OpenSSF (Linux Foundation) | accessed 2026-10-28 | 1 | **V** | Check list with purpose and risk level | none |
| S-13 | [SLSA v1.2](https://slsa.dev/spec/v1.2/), Build track basics, What's new, Source requirements (Basics); v1.0 levels kept for comparison | OpenSSF / SLSA | **v1.2 (current)**; v1.0 retired | 1 | **L** (pages listed) | Build track L0 to L3; Source track L1 to L4; changes since v1.1 | Source requirements, build requirements, principles and threats pages are downloaded but not yet reviewed in full |
| S-14 | [NIST SP 800-190, Application Container Security Guide](https://nvlpubs.nist.gov/nistpubs/SpecialPublications/NIST.SP.800-190.pdf) | NIST | final, 2017-09-25 | 1 | **C** | Full text now extracted (378,115 characters) but not yet reviewed | Pending (`ENAB-05.4`); nothing is relied upon yet |
| S-15 | [GKE: Harden your cluster](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/hardening-your-cluster) | Google Cloud | accessed 2026-10-28 | 2 | **V** | Service accounts; RBAC; network policy; Pod Security; node OS; Shielded nodes; audit logging; upgrades | Page does not cover Binary Authorization |
| S-16 | [Google Cloud Well-Architected Framework](https://docs.cloud.google.com/architecture/framework) | Google Cloud | accessed 2026-10-28 | 2 | **P** | Six pillars; security topics as listed in the navigation | Principles themselves not read |
| S-17 | [AWS Well-Architected: pillars of the framework](https://docs.aws.amazon.com/wellarchitected/latest/framework/the-pillars-of-the-framework.html) | AWS | accessed 2026-10-28 | 2 | **V** | The six pillars | none |
| S-18 | [AWS Well-Architected: Security design principles](https://docs.aws.amazon.com/wellarchitected/latest/framework/sec-design.html) | AWS | accessed 2026-10-28 | 2 | **V** | Seven design principles | none |
| S-19 | [AWS Well-Architected: Reliability design principles](https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/design-principles.html) | AWS | accessed 2026-10-28 | 2 | **V** | Five design principles | none |
| S-20 | [AWS Well-Architected: Operational Excellence design principles](https://docs.aws.amazon.com/wellarchitected/latest/framework/oe-design-principles.html) | AWS | accessed 2026-10-28 | 2 | **V** | Seven design principles | none |
| S-21 | [EKS Best Practices: Pod security](https://aws.github.io/aws-eks-best-practices/security/docs/pods/) | AWS | accessed 2026-10-28 | 2 | **V** | Policy as code and admission; Pod Security Standards; least privilege; service accounts; read-only filesystem; requests and limits | Image scanning is outside the page |
| S-22 | ISO/IEC 27001:2022, Annex A control titles, via [Open Security Architecture](https://opensecurityarchitecture.org/frameworks/iso-27001-2022) | ISO/IEC; summary by OSA | 2022 edition | 3 | **S** | Control IDs and titles only | Normative text not accessed: `iso.org` returns HTTP 403 to automated clients. The summary states its titles are not the official wording. What ISO makes publicly readable is unverified (`ENAB-05.6`) |

---

## 4. Practice catalogue

Each practice is stated in the words of this document with, where useful, a short quotation. IDs are stable; they are referenced from [section 5](#5-design-decisions).

### A. Admission control

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-ADM-01 | Prefer built-in CEL-based admission for simpler logic; use webhooks when an extensible, configurable implementation is needed. For custom resources use built-in validation and defaulting rather than webhooks. | S-01 Introduction, CRD note | Operating a webhook server adds availability and latency risk |
| BP-ADM-02 | Limit each webhook's scope: do not match `kube-system` without an `objectSelector`, do not mutate node `Lease` objects, do not match virtual authn/authz resources, and restrict with `namespaceSelector`. | S-01 Request filtering | Blocking control-plane workloads; failed node upgrades |
| BP-ADM-03 | Exclude the namespace in which the webhook runs: otherwise it can deadlock its own deployment. | S-01 Side effects and self-mutation | Circular dependency at start-up or recovery |
| BP-ADM-04 | Use `matchConditions`, which "might significantly reduce the number of calls to your webhook server"; keep `matchPolicy: Equivalent` so all API versions are covered. | S-01 Match conditions | Needless load; version bypass |
| BP-ADM-05 | Evaluate in milliseconds and use small timeouts; validating webhooks are called in parallel, mutating ones in sequence, so each mutating call adds latency. | S-01 Performance and latency | API latency; slow requests |
| BP-ADM-06 | Run several backends behind a Service for availability, and exclude operations caused by unavoidable incidents such as node downtime. | S-01 Performance and latency | Single point of failure |
| BP-ADM-07 | Operate only on the content of the `AdmissionReview`; make no out-of-band changes; declare `sideEffects: None` (or `NoneOnDryRun`). | S-01 Side effects | Dry-run requests with real effects |
| BP-ADM-08 | Mutate only the fields that need to change, do not overwrite arrays, be idempotent, and do not rely on execution order. | S-01 Mutation scope; Ordering and idempotence | Duplicated or conflicting mutations on reinvocation |
| BP-ADM-09 | Set `failurePolicy: Ignore` on mutating webhooks so downtime does not block compliant resources, and "use a validating controller to check the state of requests" to enforce the final state. | S-01 Fail open and validate the final state | Outage of a mutating component blocking the cluster |
| BP-ADM-10 | Roll out webhook configurations gradually by namespace, starting with `failurePolicy: Ignore` and a test namespace; ensure mutations do not violate validations; test minor-version upgrades in staging. | S-01 Testing; concluding guidance | Disruptive rollout; mutations rejected by later validation |
| BP-ADM-11 | Restrict who can create, update, patch or delete webhook configurations, and who can modify resources in the namespace of an in-cluster webhook server. | S-01 Limiting edit access; S-03 Admission webhook control | Tampering with the admission chain |
| BP-ADM-12 | Enforce a pod security policy through Pod Security admission and/or a webhook admission controller, and keep the admission chain securely configured. | S-04 Admission controllers | Unenforced baseline |
| BP-ADM-13 | Offer `enforce`, `audit` and `warn` modes. `audit` and `warn` apply to workload resources (Deployment, Job…) so violations surface at workload creation; `enforce` applies only to the resulting Pods. | S-06 Modes; Workload resources; S-21 Policy-as-code | Silent acceptance of a workload whose Pods will be rejected |
| BP-ADM-14 | Avoid dependency loops: webhooks that check each other's Pods, or that intercept add-ons they depend on. | S-01 Dependency loops | Deadlock |
| BP-ADM-15 | Enforce build policies both in CI and through the orchestrator's admission controllers. | S-11 Deploy | Policy applied in only one place |

| BP-ADM-16 | Do not mutate objects that cannot change, such as mirror Pods of static Pods; they carry the annotation `kubernetes.io/config.mirror`. To exclude them while limiting the security risk of ignoring an annotation, allow static Pods to run only in specific namespaces. | S-01 Don't change immutable objects | Changes that never propagate; rejected kubelet writes |
| BP-ADM-17 | Design webhooks assuming the Kubernetes API will change: do not take the stability of an API for granted (the Pod API gained `restartPolicy` for sidecars in 1.29). | S-01 Plan for future updates to fields; Importance of good webhook design | Silent breakage after a cluster upgrade |
| BP-ADM-18 | Keep a webhook from triggering itself: mark any resource the webhook creates with a unique label and exclude it from the match conditions. | S-01 Prevent your webhook from triggering itself | Self-invocation loops |
| BP-ADM-19 | Enable a webhook in order: install and start the server; set `failurePolicy: Ignore`; set `namespaceSelector` to a test namespace; deploy the configuration; monitor; then widen. Pause the rollout if it intercepts an unintended request. | S-01 Install and enable a mutating webhook | Cluster-wide disruption from a misconfigured webhook |
| BP-ADM-20 | Choose the mechanism by need: mutating or validating **webhooks** for complex logic (including external calls); **mutating admission policy** or **validating admission policy** (CEL) for simple changes such as labels or replica counts. | S-01 Choose an admission control mechanism | Needless webhook servers |

### B. Pod security

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-PSS-01 | Use three cumulative profiles: Privileged (unrestricted), Baseline (prevents known escalations), Restricted (current hardening best practice). | S-05 Profiles | Inconsistent security levels |
| BP-PSS-02 | Baseline: forbid host namespaces, privileged containers, `hostPath` volumes and host ports; restrict capabilities to an approved list; forbid `Unconfined` seccomp; restrict AppArmor, SELinux, `/proc` mount type and sysctls to safe values. | S-05 Baseline controls; Baseline sysctls | Container escape; host access |
| BP-PSS-03 | Restricted adds: volume-type allow-list; `allowPrivilegeEscalation: false`; `runAsNonRoot: true`; `runAsUser` not 0; seccomp `RuntimeDefault` or `Localhost`; drop `ALL` capabilities, adding back only `NET_BIND_SERVICE`. | S-05 Restricted controls | Privilege escalation; root execution |
| BP-PSS-04 | Set memory limits with limit equal to or below request; enable seccomp; use AppArmor or SELinux where supported. | S-04 Pod security | Node OOM; missing syscall filtering |
| BP-PSS-05 | Set requests and limits on every container and enforce them with quotas, `LimitRange` or policy as code. | S-21 Resource requests and limits | Resource contention; denial of service |
| BP-PSS-06 | Use a read-only root filesystem, never mount the container-runtime socket, and restrict `hostPath`. | S-21 Read-only filesystem; Least privilege | Tampering; host takeover |
| BP-PSS-07 | Do not exempt controller service accounts from admission: it would implicitly exempt anyone who can create the workload. | S-06 Exemptions | Policy bypass through exemption |
| BP-PSS-08 | Pod updates that change only metadata (except the seccomp and AppArmor annotations), valid `activeDeadlineSeconds`, or valid `tolerations` are exempt from policy checks, so they are not denied even when the Pod already violates the policy. | S-06 Exemptions | Locking out routine updates of existing Pods |
| BP-PSS-09 | The Restricted controls for privilege escalation, seccomp and Linux capabilities apply only when `spec.os.name` is not `windows`. Kubelets before 1.24 do not enforce the OS field. | S-05 Pod OS field | Rejecting valid Windows Pods |
| BP-PSS-10 | Pin each mode's policy to the standards of a given Kubernetes minor version with the `<mode>-version` label, so behaviour does not change on upgrade. | S-06 Labels | Policy drift across upgrades |
| BP-PSS-11 | No standard profile exists for sandboxed Pods: protections differ with the sandboxing method and no single profile is recommended for all. | S-05 FAQ: What about sandboxed Pods? | Over- or under-restricting sandboxed workloads |
| BP-PSS-12 | Enforcement alternatives to the built-in controller exist in the ecosystem (Kubewarden, Kyverno, OPA Gatekeeper); the standards are defined independently of the enforcement mechanism. | S-05 Policy Instantiation; Alternatives | Building what could be adopted |

### C. RBAC

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-RBAC-01 | Grant least privilege; prefer namespaced bindings; avoid wildcards, which also cover object types created in the future. | S-03 General good practice | Over-broad access |
| BP-RBAC-02 | Limit `cluster-admin`; do not use `system:masters`, which bypasses RBAC and cannot be revoked by removing bindings. | S-03 General good practice | Irrevocable super-user access |
| BP-RBAC-03 | `get`, `list` and `watch` on Secrets all reveal their contents. | S-03 Listing secrets | Credential disclosure |
| BP-RBAC-04 | Treat `escalate`, `bind`, `impersonate`, CSR approval and service-account token requests as privileged. | S-03 Privilege-escalation risks | Self-granted privilege |
| BP-RBAC-05 | Control over validating and mutating webhook configurations allows reading and altering admitted objects; restrict write access. | S-03 Control over admission webhooks | Interception of all objects |
| BP-RBAC-06 | `patch` on Namespaces allows changing labels, which can weaken Pod Security admission or expose services through NetworkPolicy. | S-03 Namespace modification | Policy scoping bypass through labels |
| BP-RBAC-07 | Pod-creation rights imply access to the namespace's Secrets, ConfigMaps and PVCs; enforce Baseline or Restricted. | S-03 Workload creation | Lateral access via a new Pod |
| BP-RBAC-08 | Do not mount service-account tokens in pods that do not need them; use bound tokens. | S-03 Hardening; S-04 Secrets | Token theft |
| BP-RBAC-09 | Review RBAC and access periodically (at most every 24 months). | S-03 Periodic review; S-04 Authentication and authorisation | Privilege accumulation |
| BP-RBAC-10 | Limit object counts with quotas; users able to create objects can exhaust etcd. | S-03 Denial of service | Control-plane exhaustion |

### D. Networking

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-NET-01 | A NetworkPolicy has no effect unless the network plugin enforces it. | S-07 Prerequisites | False sense of isolation |
| BP-NET-02 | Pods are non-isolated by default; policies are additive; both the source's egress and the destination's ingress must allow a connection. | S-07 Pod isolation | Unintended reachability |
| BP-NET-03 | Apply a default-deny policy per namespace covering all pods, so none is missed by an allow-list; deny-all egress also blocks DNS. | S-04 Network security; S-07 Default policies | Workloads left open |
| BP-NET-04 | Select namespaces by the immutable `kubernetes.io/metadata.name` label. | S-07 Targeting a namespace by its name | Selector evasion through mutable labels |
| BP-NET-05 | NetworkPolicy cannot do TLS, log events, apply defaults across namespaces, or express explicit denies; use a service mesh or other tooling for these. | S-07 What you can't do | Expecting guarantees the API does not give |
| BP-NET-06 | Encrypt in-cluster traffic with a service mesh where appropriate; keep the API server, kubelet and etcd off public networks. | S-04 Network security | Eavesdropping; exposure |
| BP-NET-07 | Restrict Pod-to-Pod traffic with NetworkPolicies and/or a mesh, and verify with policy logging. | S-15 Network policy | Lateral movement |

### E. Images and supply chain

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-SC-01 | Keep images minimal and run them as an unprivileged user. | S-04 Images | Attack surface |
| BP-SC-02 | Reference images by SHA-256 digest "rather than tags", or validate provenance by verifying the image signature at deploy time through admission control. | S-04 Images | Mutable or untrusted image content |
| BP-SC-03 | Do not use the `latest` tag. | S-04 Images | Non-reproducible deployments |
| BP-SC-04 | Scan images at creation and at deployment, and patch known-vulnerable software. | S-04 Images | Known vulnerabilities |
| BP-SC-05 | Scan and sign artefacts when distributing; run pre-flight checks (image signatures, policies, network policies) before deploying. | S-11 Distribute; Deploy | Tampered or unvetted artefacts |
| BP-SC-06 | Assess a project's own hygiene with the OpenSSF checks: pinned dependencies, token permissions, dangerous workflows, SAST, security policy, dependency-update tooling, signed releases, vulnerabilities, SBOM, branch protection, code review. | S-12 | Compromise of the project itself |
| BP-SC-07 | Build provenance in levels (unchanged between v1.0 and v1.2): L1 provenance exists; L2 a hosted platform signs it; L3 hardened builds isolate runs and keep signing material from user-defined steps. | S-13 (Build track basics, v1.2) | Forged or missing provenance |
| BP-SC-08 | Source track (new in v1.2): L1 use version control; L2 preserve change history and generate source provenance; L3 enforce organisational technical controls; L4 require two-party review of changes to protected branches. | S-13 (Source requirements, Basics, v1.2) | Unilateral or untraceable source changes |

### F. API and policy-model design

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-API-01 | Express CRD invariants as CEL `x-kubernetes-validations` (stable since 1.29); a rule is scoped to its schema node and to the current object; provide `message`, `reason` and `fieldPath`. | S-08 Validation rules | Invalid objects reaching controllers |
| BP-API-02 | Put `maxItems`, `maxProperties` and `maxLength` on anything a rule processes; unbounded rules are rejected as too costly; avoid nested lists and maps. | S-08 Resource use; S-09 Cost | Rule rejected or API-server exhaustion |
| BP-API-03 | Transition rules (`oldSelf`) apply only to updates; `optionalOldSelf` (stable 1.33) lets them also evaluate on create. | S-08 Transition rules | Unintended state transitions |
| BP-API-04 | Validation ratcheting (stable 1.33, on by default from 1.30) lets updates through when the invalid parts of an object are unchanged; it cannot make a valid object invalid. | S-08 Validation ratcheting | New rules locking out existing objects |
| BP-API-05 | CEL library availability depends on the cluster version (for example `quantity` from 1.29, IP and CIDR from 1.31). | S-09 Libraries | Rules unusable on older clusters |
| BP-API-06 | Policy attachment: a `targetRef` of `group`, `kind`, `name`; a hierarchy in which lower, more specific levels inherit from higher ones; deterministic conflict resolution (less specific is "established", then older timestamp, then alphabetical); explicit merge strategies; cross-namespace targeting discouraged; `Accepted` / `Conflicted` conditions. | S-10 | Ambiguous or non-deterministic policy outcomes |
| BP-API-07 | A CEL-based, in-process policy (`ValidatingAdmissionPolicy`, GA 1.30) removes the webhook server; bindings choose `Deny`, `Warn` or `Audit`. | S-02 | Webhook availability coupling |

### G. Reliability, operations and observability

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-OPS-01 | Recover from failure automatically; scale horizontally; test recovery procedures; manage change through automation. | S-19 | Single point of failure; untested recovery |
| BP-OPS-02 | Make frequent, small, reversible changes; anticipate failure; implement observability for actionable insight; automate safely with guardrails. | S-20 | Large blast radius; unobserved failure |
| BP-OPS-03 | Maintain traceability: monitor, alert and audit actions and changes. Keep audit logs protected from general access. | S-18; S-04 Logs and auditing | Unattributable changes |
| BP-OPS-04 | Define controls as code in version-controlled templates. | S-18 Automate security best practices | Configuration drift |
| BP-OPS-05 | Expose admission evaluation, error and exemption metrics. | S-06 Metrics | Blind spots in enforcement |
| BP-OPS-06 | Use audit logs to find webhooks that repeatedly perform the same action, and (with a `RequestResponse` audit rule on `patch`) to detect reinvocation loops. | S-01 Performance and latency; Prevent loops caused by competing controllers | Undetected loops and wasted calls |

### H. Security governance frameworks

| ID | Practice | Source | Risk addressed |
| :--- | :--- | :--- | :--- |
| BP-GOV-01 | Apply least privilege "at every layer of the stack"; ship secure defaults; layer controls (defence in depth). | S-11 | Single-control failure |
| BP-GOV-02 | Design security in, assume zero trust, shift security left. | S-16 security topics | Late discovery of defects |
| BP-GOV-03 | Cover six pillars: operational excellence, security, reliability, performance (efficiency or optimisation), cost optimisation, sustainability. | S-16; S-17 | Narrow, security-only design |
| BP-GOV-04 | Security design principles: strong identity foundation, traceability, security at all layers, automation, protection of data in transit and at rest, keeping people away from data, preparing for security events. | S-18 | Gaps across the security lifecycle |
| BP-GOV-05 | Trace each control to an ISMS control objective using the 2022 Annex A titles (see crosswalk, section 8). | S-22 | Unauditable controls |

---

## 5. Design decisions

*Origin* states whether the decision was derived from the practices or predates them and is corroborated by them (see [2.5](#25-limitations)).

| ID | Decision | Practices applied | Origin | Alternatives considered and trade-off |
| :--- | :--- | :--- | :--- | :--- |
| DD-01 | The Pod webhooks are the final enforcement boundary and use `failurePolicy: Fail` | BP-ADM-12, BP-ADM-15, BP-ADM-07, BP-ADM-08, BP-ADM-16, BP-ADM-19, BP-PSS-08, BP-PSS-09, BP-GOV-01 | ADR-0002 (2026-02-26); **corroborated** | `Ignore` keeps the cluster available but admits every Pod unchecked during an outage. Accepted cost: availability coupling, mitigated by DD-12. **See DEV-01.** |
| DD-02 | Each baseline chooses `Enforce`, `Warn` or `Audit` | BP-ADM-13, BP-ADM-10, BP-OPS-02 | HU-01.1 (EPIC-01); **corroborated** | A single global mode would force big-bang adoption. Per-baseline modes allow a graduated rollout. |
| DD-03 | Pod security is expressed as atomic, individually testable rules aligned with Pod Security Standards | BP-PSS-01 to BP-PSS-04, BP-PSS-06, BP-RBAC-07 | ENAB-01.1, HU-01.2, HU-01.3; **corroborated** | Delegating entirely to Pod Security admission covers the whole standard but offers no modes, hierarchy or supply-chain rules. The operator complements it. **See DEV-04, DEV-05.** |
| DD-04 | Image provenance is governed by tag, digest and registry rules evaluated in memory | BP-SC-02, BP-SC-03, BP-SC-01, BP-SC-05 | HU-01.3; **corroborated** | Signature verification at deploy needs network access and key management inside the admission path. **See DEV-06.** |
| DD-05 | Policies attach by `targetRef` or `selector` with deterministic precedence: `targetRef`, then `selector`, then namespace, then cluster; ties by priority, then name | BP-API-06 | ENAB-02.1, HU-02.1 (EPIC-02), modelled on GEP-713; **derived** | Following GEP-713 exactly (timestamp tie-break, `targetRefs`, merge strategies) was weighed against operator-controlled ordering and simplicity. **See DEV-02, DEV-03, DEV-07.** |
| DD-06 | A `ClusterSecurityBaseline` applies before namespaced baselines, cannot be weakened by them, excludes system namespaces by default, and is scoped by name rather than by mutable labels | BP-ADM-03, BP-RBAC-06, BP-NET-04, BP-PSS-07, BP-GOV-01 | HU-02.2 (EPIC-02); the **label-scoping rule was derived** from S-03 after a flaw was found in an example | Allowing label-based exemptions is convenient but lets anyone who can patch a Namespace opt out. Exemptions by name need an RBAC-controlled edit. |
| DD-07 | When a cluster baseline needs the namespace's labels and they cannot be read, admission fails instead of skipping the baseline | BP-ADM-12, BP-GOV-01 | HU-02.2; **corroborated** | Skipping would silently drop cluster policy. Cost: a failed lookup rejects the request. |
| DD-08 | Admission evaluation is in memory, reads from the informer cache and makes no external call | BP-ADM-05, BP-ADM-04 | PRIN-001, ENAB-02.1; **corroborated** | Calling out (registries, signature services) would add latency and a new availability dependency. |
| DD-09 | The most specific `WorkloadPolicy` owns each Deployment's HPA; the policy that created an HPA releases it when it stops governing; unmanaged HPAs are never modified | BP-OPS-01, BP-RBAC-01 | HU-02.1; **derived** | One owner per namespace is simple but cannot express divergent policies. Releasing prevents orphaned objects. |
| DD-10 | Early validation of `Deployment` and `StatefulSet` fails open (`Ignore`), validates only when `spec.template` changes, shares one evaluator with the Pod webhook, and starts in `warn` mode | BP-ADM-13, BP-ADM-09, BP-ADM-10, BP-ADM-04, BP-ADM-01, BP-ADM-07, BP-ADM-14, BP-API-07 | PLAN-002 and ADR-0003 (proposed, 2026-10-28); **derived** | `Fail` doubles the availability cost and blocks repair of the operator itself. `ValidatingAdmissionPolicy` removes the webhook but cannot reuse the Go rules without duplicating them. The upstream design (S-06) applies `audit`/`warn` to workload resources and `enforce` only to Pods, which the decision mirrors. |
| DD-11 | Invariants of the operator's CRDs are expressed as CEL with bounded cost | BP-ADM-01, BP-API-01 to BP-API-05 | PLAN-002; **derived** | Keeping webhook-only validation retains a `Fail`-policy dependency for editing policies. Label-selector validity is kept in a webhook for now because none of the CEL libraries reviewed (S-09) parses selectors. |
| DD-12 | Webhook configuration excludes system namespaces and the operator's own namespace; the operator runs with several replicas, a disruption budget and anti-affinity | BP-ADM-02, BP-ADM-03, BP-ADM-06, BP-ADM-14, BP-OPS-01 | ADR-0002 mitigation, PRIN-001 §5, PRE-02.3; **corroborated** | Excluding only at evaluation time does not prevent start-up deadlock; the exclusion must be in the webhook configuration itself. |
| DD-13 | The operator runs with least-privilege RBAC and a restricted Pod on a minimal image | BP-RBAC-01 to BP-RBAC-05, BP-PSS-03, BP-SC-01, BP-RBAC-08, BP-ADM-11 | Scaffold defaults and review; **corroborated** | A broader `ClusterRole` simplifies development but widens the blast radius of a compromise. |
| DD-14 | Decisions are traceable: structured logs, status conditions, events and an ISO control reference in each violation; audit annotations are planned (PLAN-002) | BP-OPS-03, BP-OPS-04, BP-OPS-05, BP-GOV-04, BP-GOV-05 | EPIC-01, EPIC-03; **partly derived** | Messages without control references are shorter but cannot be traced to an ISMS objective. |
| DD-15 | Verification is layered: unit, `envtest` integration, end-to-end, linting and a CI matrix on the minimum and latest Go | BP-ADM-10, BP-OPS-02, BP-OPS-01 | PLAN-001 §6; **corroborated** | Unit tests alone cannot show API-server behaviour such as CRD schema and CEL rejection. |

### 5.1 Practices reserved for planned work or for deployers

Several practices are catalogued but do not yet shape a decision of this solution. They are recorded so that they are not lost and so that the reason is explicit.

| Practice | Why it is not yet a decision | Where it lands |
| :--- | :--- | :--- |
| BP-NET-01, BP-NET-05, BP-NET-06 | The operator does not yet reconcile `NetworkPolicy` or mesh resources for governed workloads. They are requirements for that work: confirm the CNI enforces policy, and use the mesh for TLS because NetworkPolicy cannot. | `HU-04.1`, `HU-04.2` (EPIC-04) |
| BP-ADM-17, BP-ADM-20, BP-PSS-10, BP-PSS-11 | Recorded from the first full reads. They do not yet shape a decision; BP-ADM-20 feeds PLAN-002 question Q1 (CEL-based alternatives), BP-PSS-10 the minimum-version decision. | `PLAN-002` Q1 and Q3; `ENAB-05.2` |
| BP-ADM-18, BP-OPS-06 | The webhooks create no objects that they also match; an audit-based loop check is not yet part of the E2E suite. | `TASK-02.5.3`, `HU-02.3` |
| BP-PSS-12 | A build-versus-adopt analysis against Kubewarden, Kyverno and OPA Gatekeeper has not been written. | `ENAB-05.8` (ADR) |
| BP-SC-08 | Source-track practices apply to the operator's own repository (protected branches, two-party review), not to the product. | `ENAB-03.2` |
| BP-RBAC-10 | Object-count quotas are a cluster-administration control, not something the operator can impose. | Deployment guidance, to be written |

---

## 6. Traceability matrix

Realisation lists the artefacts that implement a decision today; "design only" means the decision is documented and approved for design but not yet built.

| Decision | Realisation | Verification |
| :--- | :--- | :--- |
| DD-01 | `internal/webhook/core/pod_webhook.go`, `pod_mutating_webhook.go`, `admission_guards.go` (update exemption, mirror Pods, dry-run); `config/webhook/manifests.yaml` (`failurePolicy: Fail`); `docs/adrs/0002-webhook-failure-policy-fail.md` | `TestPodValidatorDeniesRootPod`, `TestPodValidatorAllowsCompliantPod`, `TestPodMutatorHandleFullFlow`, `TestPodValidatorExemptsMetadataOnlyUpdates`, `TestPodValidatorStillEvaluatesRelevantUpdates`, `TestPodMutatorIgnoresUpdates`, `TestMirrorPodsAreSkippedOnlyForNodeIdentities`, `TestNoEventsOnDryRun` |
| DD-02 | `EnforcementAction` in `api/v1alpha1/securitybaseline_types.go`; action handling in `pod_webhook.go` | `TestPodValidatorWarnModeAllowsWithWarnings`, `TestPodValidatorAuditModeAllowsSilently` |
| DD-03 | `internal/webhook/core/evaluator.go` (`RuleEvaluator` rules) | `TestPSSRulesEvaluateContainers`, `TestSeccompProfileInheritsPodProfile`, `TestHostIsolationRules`, `TestPodValidatorDeniesInitContainerWithoutReadOnly`, `TestWindowsPodsSkipLinuxOnlyRules`, `TestISOControlReferences` |
| DD-04 | `SupplyChainRule` and `parseImageReference` in `evaluator.go` | `TestSupplyChainRule`, `TestParseImageReference` |
| DD-05 | `api/v1alpha1/policy_target_types.go`; `internal/policy/resolver.go`, `subject.go`, `index.go`, `validate.go` | `TestSpecificityOf`, `TestMatches`, `TestResolveOrdersBySpecificityThenPriorityThenName`, `TestSubjectForPodDerivesDeploymentFromReplicaSet`, `TestPodMutatorMoreSpecificPolicyWinsOverHigherPriority`, `TestValidatorsRejectInvalidTargeting`; integration: *persists targetRef and selector inline in the spec* |
| DD-06 | `api/v1alpha1/clustersecuritybaseline_types.go`; `applicableBaselines` / `clusterBaselines` in `pod_webhook.go`; `internal/policy/cluster.go`; `config/samples/core_v1alpha1_clustersecuritybaseline.yaml` | `TestNamespacedBaselineCannotWeakenClusterBaseline`, `TestClusterBaselineSkipsExcludedNamespaces`, `TestIsClusterExcluded`; integration: *cannot be weakened by an Audit baseline in the namespace*, *skips system namespaces, the operator namespace and explicit exclusions*, *rejects a targetRef through the CRD schema (CEL)* |
| DD-07 | `clusterBaselines` in `pod_webhook.go` | `TestClusterBaselineNamespaceSelector` (subtest *namespace lookup failure fails closed*) |
| DD-08 | `internal/policy/` (pure functions); cached client reads in the webhooks | `TestSelectPreservesItemsAndOrder`; benchmark `BenchmarkSupplyChainRule` |
| DD-09 | `reconcileDeploymentHPAs`, `governingPolicyName`, `releaseUngovernedHPA` in `internal/controller/workloadpolicy_controller.go` | Integration: *lets a targetRef policy govern its Deployment even against a higher-priority namespace-wide policy*, *removes the HPA it created once the policy stops targeting the Deployment, leaving other HPAs alone*, *returns an error when an unmanaged HPA occupies the expected name* |
| DD-10 | Design only: `0.docs/plans/PLAN-002-hu-02-3-cel-y-validacion-temprana.md`, `docs/adrs/0003-early-workload-validation-fails-open.md` (proposed) | Planned: parity test, ratcheting test, webhook-unreachable test (PLAN-002 §7) |
| DD-11 | Partly: one CEL rule on `ClusterSecurityBaselineSpec`. Remainder is design only (PLAN-002 §3) | Integration: *rejects a targetRef through the CRD schema (CEL)*; remaining rules planned |
| DD-12 | `config/webhook/patches/namespace-selector.yaml` (system, `cert-manager` and operator namespaces), `timeoutSeconds` markers, `config/manager/manager.yaml` (2 replicas, anti-affinity), `config/manager/pdb.yaml`; evaluation-level exclusion in `internal/policy/cluster.go` | `test/manifests` (`TestPodWebhooksExcludeSystemAndOperatorNamespaces`, `TestOperatorRunsHighlyAvailable`, `TestEveryWebhookHasAnExplicitShortTimeout`), `TestIsClusterExcluded`; cold-install and replica-loss E2E still to run (`TASK-02.5.3`, needs Kind) |
| DD-13 | `config/rbac/role.yaml` (read-only on the policy CRs, `events` create/patch), `config/manager/manager.yaml`, `Dockerfile` | `TestManagerMemoryLimitDoesNotExceedRequest`; manifests reviewed against S-03 and S-04 |
| DD-14 | `reconcile_status_helper.go`; `logcheck` in `.golangci.yml`; `ISOControl()` in `evaluator.go` | Integration smoke specs for the four controllers assert the `Available` condition |
| DD-15 | `internal/**/_test.go`, `test/integration/`, `test/e2e/`, `.golangci.yml`, `.github/workflows/test.yml` | The suites themselves; `make test` and `make lint` |

---

## 7. Deviations from sources

A deviation is a **deliberate** departure from a practice, with its rationale and residual risk. Unintended shortfalls are gaps and live in Annex A.

| ID | Practice departed from | What the solution does | Rationale | Residual risk and review trigger |
| :--- | :--- | :--- | :--- | :--- |
| DEV-01 | BP-ADM-09: mutating webhooks fail open and a validator checks the final state | `mpod.kb.io` uses `failurePolicy: Fail` (ADR-0002) | The mutator injects mandatory governance defaults (resources, labels, telemetry). Failing open would let Pods be created without them, and no validator currently checks those defaults. | Availability coupling to the operator. Reduced by DD-12. **Review when** a validator that verifies the defaults exists; the mutating webhook could then move to `Ignore`. |
| DEV-02 | BP-API-06: ties broken by older creation timestamp, then name | Ties broken by `priority`, then namespace and name | Operators get an explicit, reviewable ordering; creation timestamps change when GitOps tooling deletes and re-creates objects, which would silently re-order policies. | Differs from what Gateway API users expect. **Review when** interoperability with Gateway API policies is required. |
| DEV-03 | BP-API-06: plural `targetRefs`; singular `targetRef` discouraged for multi-target use | Singular `targetRef` | One policy per target keeps precedence simple. | Cannot attach one policy to several named targets. **Review when** a multi-target need arises; add `targetRefs` and deprecate the singular field. |
| DEV-04 | BP-PSS-03: add back only `NET_BIND_SERVICE` | The baseline author supplies an `allowedCapabilities` list | Some workloads need other capabilities and the decision belongs to the baseline owner. | A permissive list weakens the control. Mitigated by reviewing the baseline, not by the operator. |
| DEV-05 | BP-PSS-02, BP-PSS-03: the full Pod Security Standards | Rules cover privileged containers, privilege escalation, host namespaces, `hostPath`, capabilities, seccomp and non-root. **Not** enforced: Restricted volume-type allow-list, `runAsUser` ≠ 0, HostProcess, host ports, AppArmor, SELinux, `/proc` mount type, sysctls | The operator adds governance Pod Security admission lacks (modes per baseline, hierarchy, supply chain) and does not replace it. | A cluster relying only on this operator for Pod Security gets partial coverage. **Mitigation:** run Pod Security admission alongside. **Review when** a control is added as a rule. |
| DEV-06 | BP-SC-02: verify the image signature through admission | Digest pinning, tag rules and a registry allow-list; no signature or provenance verification | Verification needs network access and key management in the admission path (conflicts with DD-08). | A digest proves identity, not provenance. **Review when** a design for out-of-band verification exists. |
| DEV-07 | BP-API-06: merge strategy "None" (only one policy takes effect) | Every matching baseline is evaluated and their violations are unioned; defaulting policies apply with the most specific first and never overwrite; HPA ownership is single | Security baselines are obligations: combining them can only tighten. Precedence matters where values compete (defaults, HPA). | Several overlapping baselines can produce many violations. Acceptable: each is attributable to its policy. |
| DEV-08 | BP-API-06: `Accepted`, `Conflicted` and `Programmed` conditions | A single `Available` condition | Delivered incrementally. **Planned in HU-03.1.** | Conflicts are not visible in status until then. |

---

## 8. Standards crosswalk

Each row maps a criterion from a framework to the practice or decision that addresses it, or states why it is out of scope. There are no status marks here; see Annex A.

### 8.1 AWS Well-Architected Framework

| Criterion | Source | Addressed by |
| :--- | :--- | :--- |
| Security: strong identity foundation | S-18 | DD-13, BP-RBAC-01 to BP-RBAC-05 |
| Security: maintain traceability | S-18 | DD-14, BP-OPS-03 |
| Security: apply security at all layers | S-18 | DD-01, DD-03, DD-04, DD-06 |
| Security: automate security best practices | S-18 | DD-02, DD-05, BP-OPS-04 (policies as versioned CRDs) |
| Security: protect data in transit and at rest | S-18 | DD-13 (TLS for webhooks and metrics); mesh mTLS is EPIC-04 |
| Security: keep people away from data | S-18 | Out of scope: the operator handles no application data |
| Security: prepare for security events | S-18 | Not yet addressed; see Annex A |
| Reliability: recover automatically; scale horizontally; test recovery; manage change by automation | S-19 | DD-09, DD-12, DD-15, BP-OPS-01 |
| Operational excellence: safely automate; small reversible changes; anticipate failure; observability; refine procedures; learn from events | S-20 | DD-02, DD-10, DD-14, DD-15, BP-OPS-02 |
| Operational excellence: organize teams around outcomes; use managed services | S-20 | Out of scope: organisational practice |
| Cost optimisation | S-17 | DD-03 (default requests and limits), BP-PSS-05, BP-GOV-03 |
| Performance efficiency | S-17 | DD-08, BP-GOV-03 |
| Sustainability | S-17 | No dedicated practice; minimal image (BP-SC-01, BP-GOV-03) |

### 8.2 Google Cloud

| Criterion | Source | Addressed by |
| :--- | :--- | :--- |
| Enforce Pod Security Standards | S-15 | DD-03, BP-PSS-01 to BP-PSS-03 |
| Restrict ServiceAccount permissions to what the workload needs | S-15 | DD-13 |
| Restrict Pod-to-Pod traffic with NetworkPolicies or a mesh | S-15 | BP-NET-07; EPIC-04 for governed workloads |
| Keep audit logs and monitor | S-15 | DD-14, BP-OPS-03 |
| Keep components patched | S-15 | BP-SC-04, BP-SC-06 |
| Security by design, zero trust, shift-left | S-16 | DD-01, DD-10, BP-GOV-02 |
| Node-level hardening (Shielded nodes, node OS, release channels) | S-15 | Out of scope: concerns of the cluster operator |
| Binary Authorization / provenance | none | Not covered by S-15; see DEV-06 |

### 8.3 CNCF and OpenSSF

| Criterion | Source | Addressed by |
| :--- | :--- | :--- |
| Enforce policies in CI and by admission control | S-11 | DD-01, BP-ADM-15 |
| Policy as code | S-11 | DD-05, BP-OPS-04 |
| Secure defaults; least privilege at every layer; defence in depth | S-11 | DD-06, DD-13, BP-GOV-01 |
| Scan and sign artefacts; pre-flight deploy checks | S-11 | DD-04, DEV-06 |
| Project hygiene checks (pinned dependencies, token permissions, SAST, security policy, signed releases, SBOM, etc.) | S-12 | BP-SC-06; applies to the operator's own repository, see Annex A |
| Build provenance levels | S-13 | BP-SC-07; applies to the operator's own release process, see Annex A |

### 8.4 ISO/IEC 27001:2022 Annex A

Titles from S-22 (level **S**). The mapping is a proposal for the ISMS owner to confirm; the control references the operator prints today are discussed in Annex A.

| Control | Title | Addressed by |
| :--- | :--- | :--- |
| A.8.2 | Privileged access rights | DD-03 (non-root, privileged, capabilities), DD-13 |
| A.8.9 | Configuration management | DD-03, DD-05, DD-06 (security baselines as versioned configuration) |
| A.8.14 | Redundancy of information processing facilities | DD-12 |
| A.8.15 | Logging | DD-14 |
| A.8.16 | Monitoring activities | DD-14, BP-OPS-05 |
| A.8.19 | Installation of software on operational systems | DD-04 (proposed for the registry allow-list) |
| A.8.20 / A.8.22 | Networks security / Segregation of networks | BP-NET-03; EPIC-04 |
| A.8.24 | Use of cryptography | DD-13 (TLS); EPIC-04 (mesh mTLS) |
| A.8.25 / A.8.29 | Secure development life cycle / Security testing in development and acceptance | DD-15 |
| A.8.32 | Change management | DD-02, DD-10 |
| A.5.15 / A.5.18 | Access control / Access rights | DD-13, BP-RBAC-09 |
| A.5.19 to A.5.23 | Supplier relationships, ICT supply chain, cloud services | DD-04 (proposed), BP-SC-06 |

---

## Annex A. Conformance snapshot (point in time)

> **Status of this annex.** It reflects the repository on **2026-10-28** and is expected to change at every review. It does not alter the evidence in sections 1 to 8. The authoritative tracking of each item is `0.docs/backlog/BACKLOG.md`.

| ID | Finding | Practice | Severity | State (2026-10-28) | Tracked in |
| :--- | :--- | :--- | :---: | :--- | :--- |
| G-01 | The webhook configuration excludes `kube-system`, `kube-public` and `kube-node-lease` but not the operator namespace (`platform-governance-operator-system`) or `cert-manager`; no explicit `timeoutSeconds`. A cold install can deadlock. | BP-ADM-02, BP-ADM-03, BP-ADM-05 | High | **Fixed** in manifests (`test/manifests`); cold-install E2E pending (needs Kind) | `PRE-02.3` |
| G-02 | One replica, no disruption budget or anti-affinity, with `failurePolicy: Fail`. | BP-ADM-06, BP-OPS-01 | Critical | **Fixed** in manifests (2 replicas, PDB, anti-affinity); replica-loss E2E pending (needs Kind) | `PRE-02.3` |
| G-03 | The ISO references printed in violation messages (`A.8.18` for read-only root filesystem, host namespaces and `hostPath`; `A.8.30` for the supply-chain rule) do not match the control titles in S-22 (*Use of privileged utility programs*; *Outsourced development*). | BP-GOV-05 | High | **Fixed** in code (`A.8.9`, `A.5.21`); mapping awaits ISMS owner and normative text (`ENAB-05.6`) | `BUG-01.1` |
| G-04 | The operator `ClusterRole` lacks `events`; only the leader-election Role grants it, in the operator namespace. Events in workload namespaces are probably dropped (not verified on a real cluster). | BP-OPS-03 | High | **Fixed** (`events` permission added; no events on dry-run); effect on a real cluster unverified | `BUG-01.2` |
| G-05 | The operator's RBAC grants `create` and `delete` on its own CRDs that no controller uses. | BP-RBAC-01 | Medium | **Fixed** (controllers read the policy CRs only) | `ENAB-03.3` |
| G-06 | The manager's memory `limit` (128Mi) is above its `request` (64Mi). | BP-PSS-04 | Low | **Fixed** (request equals limit) | `ENAB-03.3` |
| G-07 | NetworkPolicy manifests exist but are commented out in `config/default`; there is no default-deny; the webhook policy admits only namespaces labelled `webhook: enabled`, unverified against API-server traffic. | BP-NET-02, BP-NET-03 | Medium | Open: design decision, needs a cluster to validate API-server traffic | `ENAB-03.3` |
| G-08 | Repository supply chain: actions pinned by tag, no `permissions:` blocks, no `SECURITY.md`, dependency-update tooling, SAST, SBOM or image signing; base images not pinned by digest. | BP-SC-06, BP-SC-07 | Medium | Open | `ENAB-03.2` |
| G-09 | No documented periodic access review or incident-response procedure. | BP-RBAC-09; S-18 | Low | Open | Not scheduled |
| G-10 | `WorkloadPolicy` does not validate that default requests do not exceed default limits, although mutations should not violate later validation. | BP-ADM-10 | Medium | Open | `HU-02.3` (`TASK-02.4.1`) |
| G-11 | Namespace-label scoping of `ClusterSecurityBaseline.namespaceSelector` can be evaded by anyone able to patch Namespaces. The example was corrected; the behaviour remains and needs documentation and RBAC guidance. | BP-RBAC-06, BP-NET-04 | Medium | **Fixed** (guidance in the CRD description, AGENTS.md and the corrected sample) | `HU-02.2` follow-up (PLAN-002 R-16) |
| G-13 | Both Pod webhooks run on every `UPDATE` (`verbs=create;update`) and neither inspects `req.Operation`. Pod Security admission exempts metadata-only updates; here a running Pod that violates a newly created `Enforce` baseline cannot have its labels or annotations patched. | BP-PSS-08 | High | **Fixed** (metadata-only updates exempt; mutator acts on create only) | `BUG-01.3` |
| G-14 | No rule considers `spec.os.name`; Windows Pods are evaluated against Linux-only controls (privilege escalation, seccomp, capabilities). | BP-PSS-09 | Medium | **Fixed** (Linux-only rules skipped for Windows Pods) | `BUG-01.4` |
| G-15 | Mirror Pods (`kubernetes.io/config.mirror`) are not excluded from the Pod webhooks. | BP-ADM-16 | Medium | **Fixed** (skipped only for node identities) | `BUG-01.5` |
| G-16 | The kustomize deployment enables the Pod webhooks with `failurePolicy: Fail` on all namespaces at once; the staged rollout in S-01 (Ignore plus a test namespace) is not provided or documented. | BP-ADM-19 | Medium | Partly: `timeoutSeconds` and exclusions fixed; staged-rollout runbook still to write | `PRE-02.3` (runbook task) |
| G-17 | The built-in Pod Security admission controller covers the Pod Security Standards; no analysis records why this operator is built rather than adopting Kyverno, OPA Gatekeeper or Kubewarden. | BP-PSS-12 | Medium | Open (`ENAB-05.8`) | `ENAB-05.8` |
| G-12 | A mutating webhook cannot yet be moved to `Ignore` because no validator checks the injected defaults (see DEV-01). | BP-ADM-09 | Medium | Open (review trigger of DEV-01) | Review trigger of DEV-01 |

---

## Document control

**Maintaining this document**
- Adding a practice: record the source in section 3 with its level, add the practice in section 4 with the section it came from, and reference it from the decision it informs. Never add a practice from memory.
- Changing or withdrawing a decision: update section 5 and section 6 in the same change, and record any new deviation in section 7.
- A source marked **P** or **S** must be re-read in full, or the unread portion confirmed irrelevant, before it is cited in an audit, a security review or a customer questionnaire.
- Annex A is refreshed at every review and whenever RBAC, webhook configuration, the image or CI workflows change.

**Change log**

| Version | Date | Change |
| :--- | :--- | :--- |
| 1.2 | 2026-10-28 | Defects found by the sweep fixed with regression tests: Pod `UPDATE` exemption, create-only mutation, mirror Pods, Windows Pods, dry-run events, events RBAC, least-privilege CRD verbs, ISO references, HA manifests, webhook namespace exclusions and timeouts. Annex A gains a state column. Realisation rows of DD-01, DD-03, DD-12, DD-13 updated. |
| 1.1 | 2026-10-28 | Lossless reading introduced (level **L**) with a source ledger and reproducible tooling (`hack/evidence/`). S-01 and S-06 read in full and S-05 narrative sections: 12 practices added (BP-ADM-16 to BP-ADM-20, BP-PSS-08 to BP-PSS-12, BP-SC-08, BP-OPS-06), gaps G-13 to G-17 recorded. SLSA contrasted with v1.2. ISO access recorded as blocked. |
| 1.0 | 2026-10-28 | Baseline. Replaces the earlier status checklist (`BEST-PRACTICES.md`) with an evidence document: method, source register, practice catalogue, decisions, deviations, traceability and crosswalk. Conformance findings moved to Annex A. |
