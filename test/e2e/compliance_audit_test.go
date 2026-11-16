//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platform "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
)

type auditObservation struct {
	Metadata metav1.ObjectMeta               `json:"metadata"`
	Status   platform.SecurityBaselineStatus `json:"status"`
}

func observeAudit(g Gomega, kind, name, ns, state string, violating int) auditObservation {
	GinkgoHelper()
	out, err := kubectl("get", kind, name, "-n", ns, "-o", "json")
	g.Expect(err).NotTo(HaveOccurred())
	var observed auditObservation
	g.Expect(json.Unmarshal([]byte(out), &observed)).To(Succeed())
	g.Expect(observed.Status.Compliance).NotTo(BeNil())
	g.Expect(observed.Status.Compliance.ObservedGeneration).To(Equal(observed.Metadata.Generation))
	g.Expect(observed.Status.Compliance.Complete).To(BeTrue())
	g.Expect(observed.Status.Compliance.Evaluated).To(Equal(1))
	g.Expect(observed.Status.Compliance.Violating).To(Equal(violating))
	condition := meta.FindStatusCondition(observed.Status.Conditions, "Compliant")
	g.Expect(condition).NotTo(BeNil())
	g.Expect(string(condition.Status)).To(Equal(state))
	g.Expect(condition.ObservedGeneration).To(Equal(observed.Metadata.Generation))
	return observed
}

var _ = Describe("Continuous compliance audit", func() {
	It("assesses legacy Pods, converges periodically, promotes modes and avoids unchanged writes", func() {
		ns := testNamespace("e2e-continuous-audit")
		_, err := createPod(podYAML("legacy", ns, false), false)
		Expect(err).NotTo(HaveOccurred())
		mustApply(securityBaselineYAML("posture", ns, "Audit"))
		Eventually(func(g Gomega) {
			observed := observeAudit(g, "securitybaseline", "posture", ns, "False", 1)
			g.Expect(observed.Status.Compliance.Examples).NotTo(BeEmpty())
			g.Expect(observed.Status.Compliance.Examples[0].Pod).To(Equal("legacy"))
		}).Should(Succeed())
		Eventually(func() int { return eventCount(ns, "ViolationsFound") }).Should(Equal(1))

		By("replacing the legacy Pod without editing the policy, to exercise the periodic trigger")
		_, err = kubectl("delete", "pod", "legacy", "-n", ns, "--wait=true", "--timeout=60s")
		Expect(err).NotTo(HaveOccurred())
		_, err = createPod(podYAML("legacy", ns, true), false)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			observeAudit(g, "securitybaseline", "posture", ns, "True", 0)
		}, controller.AuditInterval+time.Minute, 3*time.Second).Should(Succeed())
		Eventually(func() int { return eventCount(ns, "AssessmentPassed") }).Should(Equal(1))

		for _, action := range []string{"Warn", "Enforce"} {
			mustApply(securityBaselineYAML("posture", ns, action))
			Eventually(func(g Gomega) { observeAudit(g, "securitybaseline", "posture", ns, "True", 0) }).Should(Succeed())
		}
		waitUntilDenied(podYAML("denied", ns, false))
		By("triggering an unchanged audit through a metadata edit")
		_, err = kubectl("annotate", "securitybaseline", "posture", "-n", ns, "audit-test=recheck")
		Expect(err).NotTo(HaveOccurred())
		baseline := observeAudit(Default, "securitybaseline", "posture", ns, "True", 0)
		Consistently(func(g Gomega) {
			current := observeAudit(g, "securitybaseline", "posture", ns, "True", 0)
			g.Expect(current.Metadata.ResourceVersion).To(Equal(baseline.Metadata.ResourceVersion))
			g.Expect(eventCount(ns, "AssessmentPassed")).To(Equal(1))
		}, 10*time.Second, time.Second).Should(Succeed())
	})

	It("persists cluster-baseline compliance in the selected namespace", func() {
		ns := testNamespace("e2e-cluster-audit")
		_, err := createPod(podYAML("legacy", ns, false), false)
		Expect(err).NotTo(HaveOccurred())
		mustApply(fmt.Sprintf(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: ClusterSecurityBaseline
metadata:
  name: e2e-audit-corporate
spec:
  enforcementAction: Audit
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  namespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: %s
`, ns))
		DeferCleanup(func() {
			_, err := kubectl("delete", "clustersecuritybaseline", "e2e-audit-corporate", "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
		})
		Eventually(func(g Gomega) {
			observed := observeAudit(g, "clustersecuritybaseline", "e2e-audit-corporate", ns, "False", 1)
			g.Expect(observed.Status.Compliance.Examples[0].Namespace).To(Equal(ns))
		}).Should(Succeed())
	})
})
