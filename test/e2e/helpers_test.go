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
	// managerDeployment is the operator Deployment after the kustomize name prefix.
	managerDeployment = "platform-governance-operator-controller-manager"
	// compliantImage is preloaded on every Kind node (sandbox image) and runs as non-root,
	// so test Pods need no registry access.
	compliantImage = "registry.k8s.io/pause:3.10"
)

// kubectl runs kubectl against the e2e kubeconfig and returns combined output.
func kubectl(args ...string) (string, error) {
	return utils.Run(exec.Command("kubectl", args...))
}

// kubectlStdin runs kubectl with manifest on stdin (for `apply -f -` / `create -f -`).
func kubectlStdin(manifest string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = strings.NewReader(manifest)
	return utils.Run(cmd)
}

func mustApply(manifest string) {
	GinkgoHelper()
	_, err := kubectlStdin(manifest, "apply", "-f", "-")
	Expect(err).NotTo(HaveOccurred())
}

// testNamespace creates a namespace that is deleted when the current container ends.
func testNamespace(name string) string {
	GinkgoHelper()
	_, err := kubectl("create", "ns", name)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_, _ = kubectl("delete", "ns", name, "--ignore-not-found", "--wait=false")
	})
	return name
}

// podYAML renders a Pod. compliant=true satisfies every rule a SecurityBaseline can
// enable (non-root, read-only root fs, no escalation, drop ALL, seccomp); false
// omits all security settings, so it violates RunAsNonRoot among others.
func podYAML(name, namespace string, compliant bool) string {
	security := ""
	containerSecurity := ""
	if compliant {
		security = `
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
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:%s
  containers:
  - name: app
    image: %s%s
`, name, namespace, name, security, compliantImage, containerSecurity)
}

// createPod submits a Pod; dryRun uses a server-side dry run, which goes through
// admission (mutation and validation) without persisting anything.
func createPod(manifest string, dryRun bool) (string, error) {
	args := []string{"create", "-f", "-"}
	if dryRun {
		args = append(args, "--dry-run=server", "-o", "yaml")
	}
	return kubectlStdin(manifest, args...)
}

// securityBaselineYAML renders a namespaced baseline that requires non-root and a
// read-only root filesystem with the given enforcement action.
func securityBaselineYAML(name, namespace, action string) string {
	return fmt.Sprintf(`apiVersion: core.platform.f3nr1r.io/v1alpha1
kind: SecurityBaseline
metadata:
  name: %s
  namespace: %s
spec:
  enforcementAction: %s
  runAsNonRoot: true
  readOnlyRootFilesystem: true
`, name, namespace, action)
}

// waitUntilDenied waits until the webhook's informer cache has seen a new policy:
// a server-side dry run of the violating Pod is rejected.
func waitUntilDenied(manifest string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		out, err := createPod(manifest, true)
		g.Expect(err).To(HaveOccurred(), "expected denial, got:\n%s", out)
		g.Expect(err.Error()).To(ContainSubstring("denied the request"))
	}).Should(Succeed())
}

// eventCount returns how many events with reason exist in namespace.
func eventCount(namespace, reason string) int {
	GinkgoHelper()
	out, err := kubectl("get", "events", "-n", namespace, "--field-selector", "reason="+reason, "-o", "name")
	Expect(err).NotTo(HaveOccurred())
	return len(utils.GetNonEmptyLines(out))
}

// managerPods returns the names of the running, non-terminating operator Pods.
func managerPods() []string {
	GinkgoHelper()
	out, err := kubectl("get", "pods", "-n", namespace, "-l", "control-plane=controller-manager",
		"-o", "go-template={{ range .items }}{{ if not .metadata.deletionTimestamp }}"+
			"{{ .metadata.name }}{{ \"\\n\" }}{{ end }}{{ end }}")
	Expect(err).NotTo(HaveOccurred())
	return utils.GetNonEmptyLines(out)
}
