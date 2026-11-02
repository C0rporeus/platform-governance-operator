//go:build e2e

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

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs exercise behaviour that envtest cannot: a real API server calling the
// webhooks through a Service, cert-manager certificates, RBAC for events, the
// scheduler spreading replicas, and Pod disruption. Each container uses its own
// namespaces and removes every cluster-scoped object it creates.

var _ = Describe("Pod admission", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-admission")
		mustApply(securityBaselineYAML("strict", ns, "Enforce"))
		waitUntilDenied(podYAML("probe", ns, false))
	})

	It("denies a non-compliant Pod and names the rule, the ISO control and the policy", func() {
		_, err := createPod(podYAML("root-pod", ns, false), false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("RunAsNonRoot / ISO/IEC 27001 A.8.2"))
		Expect(err.Error()).To(ContainSubstring("ReadOnlyRootFilesystem / ISO/IEC 27001 A.8.9"))
		Expect(err.Error()).To(ContainSubstring("policy: strict"))
	})

	It("admits a compliant Pod", func() {
		_, err := createPod(podYAML("good-pod", ns, true), false)
		Expect(err).NotTo(HaveOccurred())
	})

	// BUG-01.2: without the events permission in the ClusterRole these were dropped.
	It("records a PodDenied event in the workload namespace", func() {
		Eventually(func() int { return eventCount(ns, "PodDenied") }).Should(BeNumerically(">", 0))
	})
})

var _ = Describe("Warn mode and dry run", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-warn")
		mustApply(securityBaselineYAML("advisory", ns, "Warn"))
		Eventually(func(g Gomega) {
			out, err := createPod(podYAML("probe", ns, false), true)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(ContainSubstring("Warning:"))
		}).Should(Succeed())
	})

	It("records no events for a server-side dry run", func() {
		before := eventCount(ns, "PolicyViolationWarn")
		out, err := createPod(podYAML("dry", ns, false), true)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("RunAsNonRoot"))
		Consistently(func() int { return eventCount(ns, "PolicyViolationWarn") }, 10*time.Second, time.Second).
			Should(Equal(before))
	})

	It("admits a non-compliant Pod with a warning and records an event", func() {
		out, err := createPod(podYAML("warned", ns, false), false)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("Warning:"))
		Eventually(func() int { return eventCount(ns, "PolicyViolationWarn") }).Should(BeNumerically(">", 0))
	})
})

// BUG-01.3: a baseline created after a Pod is running must not freeze the Pod.
var _ = Describe("Updates to running Pods", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-updates")
		_, err := createPod(podYAML("legacy", ns, false), false)
		Expect(err).NotTo(HaveOccurred())
		mustApply(securityBaselineYAML("strict", ns, "Enforce"))
		waitUntilDenied(podYAML("probe", ns, false))
	})

	It("allows a metadata-only update", func() {
		_, err := kubectl("label", "pod", "legacy", "-n", ns, "owner=team-a")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("annotate", "pod", "legacy", "-n", ns, "note=relabelled")
		Expect(err).NotTo(HaveOccurred())
	})

	It("still evaluates an update that changes the image", func() {
		_, err := kubectl("set", "image", "pod/legacy", "-n", ns, "app=registry.k8s.io/pause:3.9")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("RunAsNonRoot"))
	})
})

var _ = Describe("Defaults injection", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-defaults")
		mustApply(fmt.Sprintf(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: WorkloadPolicy
metadata:
  name: defaults
  namespace: %[1]s
spec:
  defaultRequests:
    cpu: 50m
    memory: 32Mi
  defaultLimits:
    memory: 64Mi
  mandatoryLabels:
    team: platform
---
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: TelemetryProfile
metadata:
  name: otel
  namespace: %[1]s
spec:
  tracingEndpoint: http://otel-collector.observability:4317
  injectEnvVars: true
  samplingRate: "0.25"
`, ns))
	})

	It("adds resources, mandatory labels and OpenTelemetry variables to new Pods", func() {
		Eventually(func(g Gomega) {
			out, err := createPod(podYAML("mutated", ns, true), true)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(ContainSubstring("team: platform"))
			g.Expect(out).To(ContainSubstring("cpu: 50m"))
			g.Expect(out).To(ContainSubstring("memory: 64Mi"))
			g.Expect(out).To(ContainSubstring("OTEL_EXPORTER_OTLP_ENDPOINT"))
			g.Expect(out).To(ContainSubstring("http://otel-collector.observability:4317"))
			g.Expect(out).To(ContainSubstring("OTEL_TRACES_SAMPLER_ARG"))
		}).Should(Succeed())
	})
})

var _ = Describe("WorkloadPolicy autoscaling", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-hpa")
		mustApply(fmt.Sprintf(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: WorkloadPolicy
metadata:
  name: scaling
  namespace: %[1]s
spec:
  horizontalScaling:
    enabledByDefault: true
    minReplicas: 2
    maxReplicas: 4
    targetCPUUtilizationPercentage: 70
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65535
      containers:
      - name: app
        image: %[2]s
        resources:
          requests:
            cpu: 10m
`, ns, compliantImage))
	})

	It("creates a managed HPA for the Deployment", func() {
		Eventually(func(g Gomega) {
			out, err := kubectl("get", "hpa", "web-pgo-hpa", "-n", ns,
				"-o", "jsonpath={.spec.minReplicas}/{.spec.maxReplicas} "+
					"{.metadata.labels.core\\.platform\\.f3nr1r\\.io/managed-hpa}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("2/4 true"))
		}).Should(Succeed())
	})

	It("removes the managed HPA when the Deployment opts out", func() {
		_, err := kubectl("annotate", "deployment", "web", "-n", ns, "core.platform.f3nr1r.io/hpa-enabled=false")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			_, err := kubectl("get", "hpa", "web-pgo-hpa", "-n", ns)
			return err
		}).Should(HaveOccurred())
	})
})

var _ = Describe("ClusterSecurityBaseline", Ordered, func() {
	var governed, exempt string

	BeforeAll(func() {
		governed = testNamespace("e2e-cluster-governed")
		exempt = testNamespace("e2e-cluster-exempt")
		mustApply(fmt.Sprintf(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: ClusterSecurityBaseline
metadata:
  name: e2e-corporate
spec:
  enforcementAction: Enforce
  runAsNonRoot: true
  readOnlyRootFilesystem: false
  excludedNamespaces:
  - %s
---
apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: SecurityBaseline
metadata:
  name: lenient
  namespace: %s
spec:
  enforcementAction: Audit
  runAsNonRoot: false
  readOnlyRootFilesystem: false
`, exempt, governed))
		DeferCleanup(func() {
			_, _ = kubectl("delete", "clustersecuritybaseline", "e2e-corporate", "--ignore-not-found")
		})
		waitUntilDenied(podYAML("probe", governed, false))
	})

	It("cannot be relaxed by a namespaced baseline", func() {
		_, err := createPod(podYAML("root", governed, false), true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("policy: cluster/e2e-corporate"))
	})

	It("does not apply to namespaces listed in excludedNamespaces", func() {
		_, err := createPod(podYAML("root", exempt, false), true)
		Expect(err).NotTo(HaveOccurred())
	})
})

// PRE-02.3 / TASK-02.5.3: the operator must stay available and must never block
// itself or cert-manager, even under a baseline their own Pods would violate.
var _ = Describe("Operator availability", Ordered, func() {
	var governed, free string

	BeforeAll(func() {
		governed = testNamespace("e2e-availability")
		free = testNamespace("e2e-availability-free")
		// No image in this suite comes from this registry, so every Pod that the
		// baseline governs is denied, including the operator and cert-manager
		// if they were not excluded.
		mustApply(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: ClusterSecurityBaseline
metadata:
  name: e2e-registry-lockdown
spec:
  enforcementAction: Enforce
  runAsNonRoot: false
  readOnlyRootFilesystem: false
  allowedRegistries:
  - registry.example.com/
  excludedNamespaces:
  - e2e-availability-free
`)
		DeferCleanup(func() {
			_, _ = kubectl("delete", "clustersecuritybaseline", "e2e-registry-lockdown", "--ignore-not-found")
		})
		waitUntilDenied(podYAML("probe", governed, true))
	})

	It("runs two ready replicas on different nodes with a PodDisruptionBudget", func() {
		out, err := kubectl("get", "deployment", managerDeployment, "-n", namespace, "-o", "jsonpath={.status.readyReplicas}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("2"))

		out, err = kubectl("get", "pods", "-n", namespace, "-l", "control-plane=controller-manager",
			"-o", "jsonpath={range .items[*]}{.spec.nodeName}{\"\\n\"}{end}")
		Expect(err).NotTo(HaveOccurred())
		nodes := strings.Fields(out)
		Expect(nodes).To(HaveLen(2))
		Expect(nodes[0]).NotTo(Equal(nodes[1]), "both replicas landed on %s", nodes[0])

		out, err = kubectl("get", "pdb", managerDeployment, "-n", namespace,
			"-o", "jsonpath={.spec.minAvailable} {.status.disruptionsAllowed}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("1 1"))
	})

	It("keeps enforcing while one replica is lost", func() {
		pods := managerPods()
		Expect(pods).To(HaveLen(2))
		_, err := kubectl("delete", "pod", pods[0], "-n", namespace, "--grace-period=0", "--force", "--wait=false")
		Expect(err).NotTo(HaveOccurred())

		By("denying a violating Pod through the surviving replica (no fail-open)")
		Eventually(func(g Gomega) {
			_, err := createPod(podYAML("during-outage", governed, true), true)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring("SupplyChain"))
		}, 30*time.Second, time.Second).Should(Succeed())

		By("admitting Pods in namespaces the baseline does not govern")
		Eventually(func() error {
			_, err := createPod(podYAML("during-outage", free, true), true)
			return err
		}, 30*time.Second, time.Second).ShouldNot(HaveOccurred())

		_, err = kubectl("rollout", "status", "deployment/"+managerDeployment, "-n", namespace, "--timeout=3m")
		Expect(err).NotTo(HaveOccurred())
	})

	It("can restart itself under a baseline its own Pods violate", func() {
		_, err := kubectl("rollout", "restart", "deployment/"+managerDeployment, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", "deployment/"+managerDeployment, "-n", namespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForWebhooks()
	})

	It("does not block cert-manager from restarting", func() {
		_, err := kubectl("rollout", "restart", "deployment/cert-manager-webhook", "-n", "cert-manager")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", "deployment/cert-manager-webhook", "-n", "cert-manager", "--timeout=3m")
		Expect(err).NotTo(HaveOccurred())
	})
})
