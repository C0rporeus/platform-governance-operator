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
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/f3nr1r/platform-governance-operator/test/utils"
)

const (
	operatorIdentity = "system:serviceaccount:" + namespace + ":" + managerDeployment
	dryRunLabel      = "e2e-dryrun"
	safeguardName    = "platform-governance-operator-dryrun-only"
)

// rootSidecarPolicy simulates a service mesh injector: it adds a container that runs as
// root to every Pod created in the namespaces labelled e2e-dryrun=true. The Pod template
// is compliant; only the real Pod, after mutation, is not.
const rootSidecarPolicy = `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicy
metadata:
  name: e2e-root-sidecar
spec:
  failurePolicy: Fail
  reinvocationPolicy: Never
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["pods"]
  mutations:
  - patchType: JSONPatch
    jsonPatch:
      expression: >
        [JSONPatch{
          op: "add", path: "/spec/containers/-",
          value: Object.spec.containers{
            name: "istio-init",
            image: "registry.k8s.io/pause:3.10",
            securityContext: Object.spec.containers.securityContext{runAsUser: 0}
          }
        }]
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: e2e-root-sidecar
spec:
  policyName: e2e-root-sidecar
  matchResources:
    namespaceSelector:
      matchLabels:
        ` + dryRunLabel + `: "true"
`

func compliantDeploymentYAML(name, ns string, compliant bool) string {
	podSecurity, containerSecurity := "", ""
	if compliant {
		podSecurity = `
      securityContext:
        runAsNonRoot: true
        runAsUser: 65535
        seccompProfile:
          type: RuntimeDefault`
		containerSecurity = `
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: ["ALL"]`
	}
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
spec:
  selector:
    matchLabels: {app: %s}
  template:
    metadata:
      labels: {app: %s}
    spec:%s
      containers:
      - name: app
        image: %s%s
`, name, ns, name, name, podSecurity, compliantImage, containerSecurity)
}

func dryRunApply(manifest string) (string, error) {
	return kubectlStdin(manifest, "apply", "--dry-run=server", "-f", "-")
}

func operatorLogs() string {
	GinkgoHelper()
	out, err := kubectl("logs", "-n", namespace, "-l", "control-plane=controller-manager", "--tail=-1", "--prefix")
	Expect(err).NotTo(HaveOccurred())
	return out
}

func rolloutOperator() {
	GinkgoHelper()
	_, err := kubectl("rollout", "status", "deployment/"+managerDeployment, "-n", namespace, "--timeout=5m")
	Expect(err).NotTo(HaveOccurred())
	waitForWebhooks()
}

// ADR-0004: the early validation can also send a synthetic Pod through the whole admission
// chain. The mode is opt-in, needs its own permission and safeguard, and must never be
// stricter than the template check except for our own baselines in enforce mode.
var _ = Describe("Early validation with dry-run fidelity", Ordered, func() {
	var sidecarNS, pssNS string

	BeforeAll(func() {
		By("the default install cannot create Pods")
		out, _ := kubectl("auth", "can-i", "create", "pods", "--all-namespaces", "--as="+operatorIdentity)
		Expect(strings.TrimSpace(out)).To(Equal("no"), "config/default must not grant create on pods")

		By("installing the dry-run overlay")
		_, err := utils.Run(exec.Command("make", "deploy-dry-run", "IMG="+managerImage))
		Expect(err).NotTo(HaveOccurred())
		rolloutOperator()
		Eventually(operatorLogs).Should(ContainSubstring("Dry-run fidelity enabled"))

		sidecarNS = testNamespace("e2e-dryrun-sidecar")
		pssNS = testNamespace("e2e-dryrun-pss")
		for _, n := range []string{sidecarNS, pssNS} {
			_, err = kubectl("label", "ns", n, dryRunLabel+"=true")
			Expect(err).NotTo(HaveOccurred())
		}
		// Enforce in the sidecar namespace: our own rejection of the mutated Pod is what gets
		// reported. Warn in the other, so the verdict that stands out is Pod Security Admission's.
		mustApply(securityBaselineYAML("strict", sidecarNS, "Enforce"))
		mustApply(securityBaselineYAML("strict", pssNS, "Warn"))
		_, err = kubectl("label", "ns", pssNS, "pod-security.kubernetes.io/enforce=restricted")
		Expect(err).NotTo(HaveOccurred())
		mustApply(rootSidecarPolicy)

		DeferCleanup(func() {
			deleteIfPresent := func(kinds, name string) {
				_, _ = kubectl("delete", kinds, name, "--ignore-not-found")
			}
			deleteIfPresent("mutatingadmissionpolicybinding,mutatingadmissionpolicy", "e2e-root-sidecar")
			// Back to the default install: later specs expect the unprivileged operator.
			deleteIfPresent("validatingadmissionpolicybinding,validatingadmissionpolicy", safeguardName)
			deleteIfPresent("clusterrolebinding,clusterrole", "platform-governance-operator-dryrun")
			_, err := utils.Run(exec.Command("make", "deploy", "IMG="+managerImage))
			Expect(err).NotTo(HaveOccurred())
			rolloutOperator()
		})
	})

	It("reports a violation that only exists after a mutating webhook ran, without rejecting in warn mode", func() {
		Eventually(func(g Gomega) {
			out, err := dryRunApply(compliantDeploymentYAML("web", sidecarNS, true))
			g.Expect(err).NotTo(HaveOccurred(), "warn mode must never reject:\n%s", out)
			g.Expect(out).To(ContainSubstring("[early]"))
			g.Expect(out).To(ContainSubstring("istio-init"))
			g.Expect(out).To(ContainSubstring("after admission mutation"))
		}).Should(Succeed())
	})

	It("forwards what other admission controllers would say, as warnings that never reject", func() {
		Eventually(func(g Gomega) {
			out, err := dryRunApply(compliantDeploymentYAML("web", pssNS, true))
			g.Expect(err).NotTo(HaveOccurred(), "a third party's verdict must never reject:\n%s", out)
			g.Expect(out).To(ContainSubstring("the admission chain would reject the Pods"))
			g.Expect(out).To(ContainSubstring("PodSecurity"))
		}).Should(Succeed())
	})

	It("does not repeat a violation the template check already reported", func() {
		Eventually(func(g Gomega) {
			out, err := dryRunApply(compliantDeploymentYAML("web", sidecarNS, false))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.Count(out, "app container 'app' must run as non-root")).To(Equal(1),
				"the same violation must be reported once:\n%s", out)
		}).Should(Succeed())
	})

	It("keeps the operator unable to create a real Pod, even though it may create dry-run Pods", func() {
		// A namespace with no baseline and no sidecar policy: the verdict can only come from the safeguard.
		plainNS := testNamespace("e2e-dryrun-plain")
		pod := podYAML("escape", plainNS, true)
		out, err := kubectlStdin(pod, "create", "-f", "-", "--as="+operatorIdentity)
		Expect(err).To(HaveOccurred(), "the operator identity created a real Pod:\n%s", out)
		Expect(err.Error()).To(ContainSubstring("only as a server-side dry run"))
		_, err = kubectl("get", "pod", "escape", "-n", plainNS)
		Expect(err).To(HaveOccurred(), "nothing may be persisted")

		out, err = kubectlStdin(pod, "create", "--dry-run=server", "-f", "-", "--as="+operatorIdentity)
		Expect(err).NotTo(HaveOccurred(), "a dry run by the operator identity must be allowed:\n%s", out)
	})

	It("falls back to the template check when the safeguard is missing, and keeps reporting template violations", func() {
		_, err := kubectl("delete", "validatingadmissionpolicybinding", safeguardName)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "restart", "deployment/"+managerDeployment, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		rolloutOperator()
		Eventually(operatorLogs).Should(ContainSubstring("Dry-run fidelity NOT enabled"))

		Eventually(func(g Gomega) {
			out, err := dryRunApply(compliantDeploymentYAML("web", sidecarNS, true))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).NotTo(ContainSubstring("istio-init"), "without the safeguard the dry run must not run:\n%s", out)
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			out, err := dryRunApply(compliantDeploymentYAML("bad", sidecarNS, false))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(ContainSubstring("must run as non-root"))
		}).Should(Succeed())
	})
})
