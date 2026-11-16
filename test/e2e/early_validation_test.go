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
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/f3nr1r/platform-governance-operator/test/utils"
)

// TASK-02.4.4: the template-level early validation (the default install, no dry-run overlay)
// against a real API server. The dry-run fidelity is covered in dryrun_fidelity_test.go; here
// the contract is that apply-time feedback and Pod admission agree, in both modes.

// setEarlyMode restarts the operator with the given early-validation mode and waits for it.
func setEarlyMode(mode string) {
	GinkgoHelper()
	_, err := kubectl("patch", "deployment/"+managerDeployment, "-n", namespace, "--type=json", "-p",
		`[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--early-validation-mode=`+mode+`"}]`)
	Expect(err).NotTo(HaveOccurred())
	rolloutOperator()
}

var _ = Describe("Early validation of Deployments", Ordered, func() {
	var ns string

	BeforeAll(func() {
		ns = testNamespace("e2e-early")
		mustApply(securityBaselineYAML("strict", ns, "Enforce"))
		waitUntilDenied(podYAML("probe", ns, false))
		DeferCleanup(func() {
			// Back to the default install: later specs expect the default early mode.
			_, err := utils.Run(exec.Command("make", "deploy", "IMG="+managerImage))
			Expect(err).NotTo(HaveOccurred())
			rolloutOperator()
			// The rollout being complete does not mean every webhook call is answered yet:
			// wait until a real admission request round-trips before the next container starts.
			Eventually(func() error {
				_, e := kubectlStdin(securityBaselineYAML("readiness", ns, "Audit"), "apply", "--dry-run=server", "-f", "-")
				return e
			}, "2m", "3s").Should(Succeed())
		})
	})

	Context("warn mode (default)", func() {
		It("reports the violations of a Deployment as warnings and still admits it", func() {
			out, err := kubectlStdin(compliantDeploymentYAML("legacy", ns, false), "apply", "-f", "-")
			Expect(err).NotTo(HaveOccurred(), "warn mode must never reject:\n%s", out)
			Expect(out).To(ContainSubstring("Warning:"))
			Expect(out).To(ContainSubstring("[early]"))
			Expect(out).To(ContainSubstring("RunAsNonRoot"))
		})

		It("names the same rules Pod admission names for the same template", func() {
			_, podErr := createPod(podYAML("same-rules", ns, false), false)
			Expect(podErr).To(HaveOccurred())
			out, err := dryRunApply(compliantDeploymentYAML("parity", ns, false))
			Expect(err).NotTo(HaveOccurred())
			for _, rule := range []string{"RunAsNonRoot", "ReadOnlyRootFilesystem"} {
				Expect(podErr.Error()).To(ContainSubstring(rule))
				Expect(out).To(ContainSubstring(rule), "early validation must agree with Pod admission on %s", rule)
			}
		})

		It("admits a compliant Deployment without a warning", func() {
			out, err := kubectlStdin(compliantDeploymentYAML("good", ns, true), "apply", "-f", "-")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).NotTo(ContainSubstring("Warning:"))
		})

		It("does not re-evaluate a change that leaves the Pod template alone", func() {
			out, err := kubectl("scale", "deployment/legacy", "-n", ns, "--replicas=0")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).NotTo(ContainSubstring("[early]"))
		})
	})

	Context("enforce mode", Ordered, func() {
		BeforeAll(func() { setEarlyMode("enforce") })

		It("rejects at apply time what Pod admission would reject, naming rule and policy", func() {
			Eventually(func(g Gomega) {
				out, err := kubectlStdin(compliantDeploymentYAML("blocked", ns, false), "apply", "-f", "-")
				g.Expect(err).To(HaveOccurred(), "the Deployment should have been rejected:\n%s", out)
				g.Expect(err.Error()).To(ContainSubstring("[early]"))
				g.Expect(err.Error()).To(ContainSubstring("RunAsNonRoot / ISO/IEC 27001 A.8.2"))
				g.Expect(err.Error()).To(ContainSubstring("policy: strict"))
			}).Should(Succeed())
			_, err := kubectl("get", "deployment/blocked", "-n", ns)
			Expect(err).To(HaveOccurred(), "a rejected Deployment must not exist")
		})

		It("admits a compliant Deployment", func() {
			_, err := kubectlStdin(compliantDeploymentYAML("good-enforced", ns, true), "apply", "-f", "-")
			Expect(err).NotTo(HaveOccurred())
		})

		It("keeps an already-stored violating Deployment scalable (no stranded objects)", func() {
			_, err := kubectl("scale", "deployment/legacy", "-n", ns, "--replicas=1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects the change that introduces the violation, not the one that precedes it", func() {
			_, err := kubectl("set", "image", "deployment/good-enforced", "-n", ns, "app=registry.k8s.io/pause:3.9")
			Expect(err).NotTo(HaveOccurred(), "a still-compliant template change is allowed")
			_, err = kubectl("patch", "deployment/good-enforced", "-n", ns, "--type=json", "-p",
				`[{"op":"replace","path":"/spec/template/spec/securityContext/runAsNonRoot","value":false}]`)
			Expect(err).To(HaveOccurred())
			Expect(strings.ToLower(err.Error())).To(ContainSubstring("runasnonroot"))
		})
	})
})
