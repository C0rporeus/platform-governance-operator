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
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/f3nr1r/platform-governance-operator/test/utils"
)

var (
	// managerImage is the manager image to be built and loaded for testing.
	managerImage = "example.com/platform-governance-operator:v0.0.1"
	// shouldCleanupCertManager tracks whether CertManager was installed by this suite.
	shouldCleanupCertManager = false
)

// TestE2E runs the e2e test suite to validate the solution in an isolated environment.
// The default setup requires Kind and CertManager.
//
// To skip CertManager installation, set: CERT_MANAGER_INSTALL_SKIP=true
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting platform-governance-operator e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	guardAgainstNonKindCluster()

	By("building the manager image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager image")

	By("loading the manager image on Kind")
	err = utils.LoadImageToKindClusterWithName(managerImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager image into Kind")

	setupCertManager()
	deployOperator()
})

var _ = AfterSuite(func() {
	undeployOperator()
	teardownCertManager()
})

// guardAgainstNonKindCluster aborts the suite unless kubectl points at the
// disposable Kind cluster through a dedicated kubeconfig. The suite installs CRDs,
// webhooks with failurePolicy: Fail and cluster-wide baselines; running it against
// a shared cluster would block workloads there.
func guardAgainstNonKindCluster() {
	By("verifying that kubectl targets the disposable Kind cluster")
	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).NotTo(BeEmpty(),
		"KUBECONFIG must point to the e2e kubeconfig; run the suite with `make test-e2e`")
	home, _ := os.UserHomeDir()
	Expect(kubeconfig).NotTo(ContainSubstring(home+"/.kube/"),
		"refusing to run e2e against your personal kubeconfig")

	cluster := os.Getenv("KIND_CLUSTER")
	Expect(cluster).NotTo(BeEmpty(), "KIND_CLUSTER must be set")
	out, err := utils.Run(exec.Command("kubectl", "config", "current-context"))
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(out)).To(Equal("kind-"+cluster),
		"refusing to run e2e against a context other than the Kind test cluster")
}

// deployOperator installs CRDs and the operator, then waits until both replicas are
// ready and the webhooks have a CA bundle, so specs start from a working install.
func deployOperator() {
	By("creating manager namespace")
	_, err := utils.Run(exec.Command("kubectl", "create", "ns", namespace))
	Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

	By("labeling the namespace to enforce the restricted security policy")
	_, err = utils.Run(exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		"pod-security.kubernetes.io/enforce=restricted"))
	Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

	By("installing CRDs")
	_, err = utils.Run(exec.Command("make", "install"))
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	_, err = utils.Run(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage)))
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

	By("waiting for the controller-manager rollout")
	_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "deployment/"+managerDeployment,
		"-n", namespace, "--timeout=5m"))
	Expect(err).NotTo(HaveOccurred(), "controller-manager did not become ready")

	waitForWebhooks()
}

// waitForWebhooks waits until the webhook Service has endpoints and both webhook
// configurations carry the CA bundle injected by cert-manager, then proves that
// the API server can actually reach the webhook with a server-side dry run.
func waitForWebhooks() {
	By("waiting for webhook endpoints and CA injection")
	Eventually(func(g Gomega) {
		out, err := utils.Run(exec.Command("kubectl", "get", "endpointslices.discovery.k8s.io", "-n", namespace,
			"-l", "kubernetes.io/service-name=platform-governance-operator-webhook-service",
			"-o", "jsonpath={range .items[*]}{range .endpoints[*]}{.addresses[*]}{end}{end}"))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(out).NotTo(BeEmpty())
		for _, kind := range []string{"mutating", "validating"} {
			out, err = utils.Run(exec.Command("kubectl", "get", kind+"webhookconfigurations",
				"platform-governance-operator-"+kind+"-webhook-configuration",
				"-o", "go-template={{ range .webhooks }}{{ if not .clientConfig.caBundle }}missing{{ end }}{{ end }}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).NotTo(ContainSubstring("missing"), "%s webhooks without CA bundle", kind)
		}
	}, 3*time.Minute, 2*time.Second).Should(Succeed())

	By("waiting until the API server can call the webhooks")
	Eventually(func(g Gomega) {
		// A denial also proves the webhook answered (a baseline may govern "default");
		// only a failed call (connection refused, TLS, timeout) means not ready.
		_, err := utils.Run(exec.Command("kubectl", "run", "webhook-probe", "-n", "default",
			"--image="+compliantImage, "--restart=Never", "--dry-run=server"))
		if err != nil {
			g.Expect(err.Error()).To(ContainSubstring("denied the request"))
		}
	}, 2*time.Minute, 2*time.Second).Should(Succeed())
}

func undeployOperator() {
	By("undeploying the controller-manager")
	_, _ = utils.Run(exec.Command("make", "undeploy", "ignore-not-found=true"))
	By("uninstalling CRDs")
	_, _ = utils.Run(exec.Command("make", "uninstall", "ignore-not-found=true"))
	By("removing manager namespace")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found"))
}

// setupCertManager installs CertManager if needed for webhook tests.
// Skips installation if CERT_MANAGER_INSTALL_SKIP=true or if already present.
func setupCertManager() {
	if os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true" {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping CertManager installation (CERT_MANAGER_INSTALL_SKIP=true)\n")
		return
	}

	By("checking if CertManager is already installed")
	if utils.IsCertManagerCRDsInstalled() {
		_, _ = fmt.Fprintf(GinkgoWriter, "CertManager is already installed. Skipping installation.\n")
		return
	}

	// Mark for cleanup before installation to handle interruptions and partial installs.
	shouldCleanupCertManager = true

	By("installing CertManager")
	Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
}

// teardownCertManager uninstalls CertManager if it was installed by setupCertManager.
// This ensures we only remove what we installed.
func teardownCertManager() {
	if !shouldCleanupCertManager {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping CertManager cleanup (not installed by this suite)\n")
		return
	}

	By("uninstalling CertManager")
	utils.UninstallCertManager()
}
