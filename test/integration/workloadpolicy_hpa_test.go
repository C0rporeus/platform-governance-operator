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

package integration

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
)

// Public contract strings exposed by the operator through its annotations and labels.
// These are part of the documented API surface and safe to reference in black-box tests.
const (
	hpaEnabledAnnotation       = "core.platform.f3nr1r.io/hpa-enabled"
	managedHPALabel            = "core.platform.f3nr1r.io/managed-hpa"
	managedHPALabelValue       = "true"
	managedHPAPolicyAnnotation = "core.platform.f3nr1r.io/workload-policy"
)

// hpaName mirrors the operator's HPA naming convention for assertion purposes.
func hpaName(deploymentName string) string {
	const suffix = "-pgo-hpa"
	maxBase := 63 - len(suffix)
	if len(deploymentName) > maxBase {
		deploymentName = deploymentName[:maxBase]
	}
	return deploymentName + suffix
}

// hpaIntegSeq provides unique namespace suffixes within a single suite run.
var hpaIntegSeq int

var _ = Describe("WorkloadPolicy HPA Integration", func() {
	var (
		testCtx    context.Context
		testNs     string
		reconciler *controller.WorkloadPolicyReconciler
	)

	BeforeEach(func() {
		hpaIntegSeq++
		testCtx = context.Background()
		testNs = fmt.Sprintf("hpa-integ-%04d", hpaIntegSeq)

		Expect(k8sClient.Create(testCtx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: testNs},
		})).To(Succeed())

		reconciler = &controller.WorkloadPolicyReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
		}
	})

	AfterEach(func() {
		_ = k8sClient.DeleteAllOf(testCtx, &corev1alpha1.WorkloadPolicy{}, client.InNamespace(testNs))
		_ = k8sClient.DeleteAllOf(testCtx, &appsv1.Deployment{}, client.InNamespace(testNs))
		_ = k8sClient.DeleteAllOf(testCtx, &autoscalingv2.HorizontalPodAutoscaler{}, client.InNamespace(testNs))
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Policy priority — observable through HPA presence/absence
	// ─────────────────────────────────────────────────────────────────────────

	Context("Policy priority resolution", func() {
		It("creates an HPA when the policy is the only one with HPA config", func() {
			policy := integPolicy(testNs, "only-policy", 10, true)
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)

			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue(),
				"single HPA-enabled policy should produce a managed HPA")
		})

		It("only the higher-priority policy creates HPAs; the lower-priority one does not", func() {
			high := integPolicy(testNs, "high-priority", 100, true)
			low := integPolicy(testNs, "low-priority", 1, true)
			Expect(k8sClient.Create(testCtx, high)).To(Succeed())
			Expect(k8sClient.Create(testCtx, low)).To(Succeed())
			deployment := integDeployment(testNs, "app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			// Reconcile the lower-priority policy first; must NOT create HPA
			reconcilePolicy(testCtx, reconciler, testNs, low.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeFalse(),
				"lower-priority policy must not create an HPA")

			// Reconcile the higher-priority policy; MUST create HPA
			reconcilePolicy(testCtx, reconciler, testNs, high.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue(),
				"higher-priority policy must create an HPA")
		})

		It("breaks priority ties by selecting the alphabetically earliest policy name", func() {
			// Same priority: "alpha-policy" < "beta-policy" → alpha wins
			alpha := integPolicy(testNs, "alpha-policy", 50, true)
			beta := integPolicy(testNs, "beta-policy", 50, true)
			Expect(k8sClient.Create(testCtx, alpha)).To(Succeed())
			Expect(k8sClient.Create(testCtx, beta)).To(Succeed())
			deployment := integDeployment(testNs, "app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			// beta reconcile first: NOT highest (alpha wins tie-break)
			reconcilePolicy(testCtx, reconciler, testNs, beta.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeFalse(),
				"beta-policy should lose the tie-break to alpha-policy")

			// alpha reconcile: IS highest
			reconcilePolicy(testCtx, reconciler, testNs, alpha.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue(),
				"alpha-policy should win the tie-break and create an HPA")
		})

		It("ignores policies without HPA config when evaluating priority", func() {
			// No-HPA policy has a much higher priority number but no HorizontalScaling spec
			noHPA := &corev1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "no-hpa", Namespace: testNs},
				Spec: corev1alpha1.WorkloadPolicySpec{
					Priority:        999,
					MandatoryLabels: map[string]string{"team": "platform"},
				},
			}
			withHPA := integPolicy(testNs, "with-hpa", 1, true)
			Expect(k8sClient.Create(testCtx, noHPA)).To(Succeed())
			Expect(k8sClient.Create(testCtx, withHPA)).To(Succeed())
			deployment := integDeployment(testNs, "app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, withHPA.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue(),
				"policy with HPA config should be highest among HPA-capable policies")
		})

		It("does not create an HPA when the policy has no HPA config", func() {
			noHPA := &corev1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "no-hpa-config", Namespace: testNs},
				Spec:       corev1alpha1.WorkloadPolicySpec{Priority: 10},
			}
			Expect(k8sClient.Create(testCtx, noHPA)).To(Succeed())
			deployment := integDeployment(testNs, "app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, noHPA.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeFalse())
		})
	})

	// ─────────────────────────────────────────────────────────────────────────
	// HPA lifecycle
	// ─────────────────────────────────────────────────────────────────────────

	Context("HPA lifecycle", func() {
		It("creates a managed HPA with correct spec and metadata", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, true)
			policy.Spec.HorizontalScaling.MinReplicas = 2
			policy.Spec.HorizontalScaling.MaxReplicas = 8
			policy.Spec.HorizontalScaling.TargetCPUUtilizationPercentage = 70
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)

			hpa := fetchHPA(testCtx, testNs, deployment.Name)
			Expect(hpa.Labels[managedHPALabel]).To(Equal(managedHPALabelValue))
			Expect(hpa.Annotations[managedHPAPolicyAnnotation]).To(Equal(policy.Name))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(8)))
			Expect(hpa.Spec.MinReplicas).NotTo(BeNil())
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(2)))
			Expect(hpa.OwnerReferences).To(HaveLen(1))
			Expect(hpa.OwnerReferences[0].Kind).To(Equal("WorkloadPolicy"))
		})

		It("does not create an HPA when enabledByDefault is false and no annotation", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, false) // enabledByDefault=false
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeFalse())
		})

		It("creates an HPA when the Deployment annotation explicitly enables it", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, false) // default off
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", map[string]string{
				hpaEnabledAnnotation: "true",
			})
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue())
		})

		It("updates an existing managed HPA when the policy MaxReplicas drifts", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, true)
			policy.Spec.HorizontalScaling.MaxReplicas = 5
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(fetchHPA(testCtx, testNs, deployment.Name).Spec.MaxReplicas).To(Equal(int32(5)))

			// Update policy (fetch first to get current ResourceVersion)
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: policy.Name, Namespace: testNs}, policy)).To(Succeed())
			policy.Spec.HorizontalScaling.MaxReplicas = 15
			Expect(k8sClient.Update(testCtx, policy)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(fetchHPA(testCtx, testNs, deployment.Name).Spec.MaxReplicas).To(Equal(int32(15)))
		})

		It("deletes a managed HPA when the Deployment annotation disables scaling", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, true)
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeTrue())

			// Disable via annotation
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: deployment.Name, Namespace: testNs}, deployment)).To(Succeed())
			if deployment.Annotations == nil {
				deployment.Annotations = map[string]string{}
			}
			deployment.Annotations[hpaEnabledAnnotation] = "false"
			Expect(k8sClient.Update(testCtx, deployment)).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(hpaExists(testCtx, testNs, deployment.Name)).To(BeFalse(),
				"managed HPA must be deleted when annotation disables it")
		})

		It("returns an error when an unmanaged HPA occupies the expected name", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, true)
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", nil)
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			// Pre-create an HPA without the managed label
			Expect(k8sClient.Create(testCtx, &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: hpaName(deployment.Name), Namespace: testNs},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name,
					},
					MaxReplicas: 5,
				},
			})).To(Succeed())

			_, err := reconciler.Reconcile(testCtx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: policy.Name, Namespace: testNs},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not managed by platform-governance-operator"))
		})

		It("returns an error for a Deployment with an invalid HPA annotation value", func() {
			policy := integPolicy(testNs, "hpa-policy", 10, true)
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			deployment := integDeployment(testNs, "my-app", map[string]string{
				hpaEnabledAnnotation: "maybe",
			})
			Expect(k8sClient.Create(testCtx, deployment)).To(Succeed())

			_, err := reconciler.Reconcile(testCtx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: policy.Name, Namespace: testNs},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid"))
		})
	})
})

// ─── helpers ─────────────────────────────────────────────────────────────────

func integPolicy(namespace, name string, priority int32, enabledByDefault bool) *corev1alpha1.WorkloadPolicy {
	return &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1alpha1.WorkloadPolicySpec{
			Priority: priority,
			HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{
				EnabledByDefault:               enabledByDefault,
				MinReplicas:                    2,
				MaxReplicas:                    10,
				TargetCPUUtilizationPercentage: 80,
			},
		},
	}
}

func integDeployment(namespace, name string, annotations map[string]string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}}},
			},
		},
	}
}

func reconcilePolicy(ctx context.Context, r *controller.WorkloadPolicyReconciler, namespace, name string) {
	GinkgoHelper()
	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	})
	Expect(err).NotTo(HaveOccurred())
}

func hpaExists(ctx context.Context, namespace, deploymentName string) bool {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: hpaName(deploymentName), Namespace: namespace}, hpa)
	return !apierrors.IsNotFound(err)
}

func fetchHPA(ctx context.Context, namespace, deploymentName string) *autoscalingv2.HorizontalPodAutoscaler {
	GinkgoHelper()
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      hpaName(deploymentName),
		Namespace: namespace,
	}, hpa)).To(Succeed())
	return hpa
}
