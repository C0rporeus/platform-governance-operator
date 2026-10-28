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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
)

var _ = Describe("Policy targeting", func() {
	var (
		testCtx    context.Context
		testNs     string
		reconciler *controller.WorkloadPolicyReconciler
	)

	BeforeEach(func() {
		hpaIntegSeq++
		testCtx = context.Background()
		testNs = fmt.Sprintf("target-integ-%04d", hpaIntegSeq)

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
		_ = k8sClient.DeleteAllOf(testCtx, &corev1alpha1.SecurityBaseline{}, client.InNamespace(testNs))
		_ = k8sClient.DeleteAllOf(testCtx, &appsv1.Deployment{}, client.InNamespace(testNs))
		_ = k8sClient.DeleteAllOf(testCtx, &autoscalingv2.HorizontalPodAutoscaler{}, client.InNamespace(testNs))
	})

	Context("HPA governance with divergent policies in one namespace", func() {
		It("lets a targetRef policy govern its Deployment even against a higher-priority namespace-wide policy", func() {
			wide := integPolicy(testNs, "wide", 100, true)
			apiOnly := integPolicy(testNs, "api-only", 0, true)
			apiOnly.Spec.HorizontalScaling.MaxReplicas = 20
			apiOnly.Spec.TargetRef = &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}
			Expect(k8sClient.Create(testCtx, wide)).To(Succeed())
			Expect(k8sClient.Create(testCtx, apiOnly)).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "api", nil))).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "web", nil))).To(Succeed())

			// Result must not depend on reconcile order.
			reconcilePolicy(testCtx, reconciler, testNs, apiOnly.Name)
			reconcilePolicy(testCtx, reconciler, testNs, wide.Name)
			reconcilePolicy(testCtx, reconciler, testNs, apiOnly.Name)

			apiHPA := fetchHPA(testCtx, testNs, "api")
			Expect(apiHPA.Spec.MaxReplicas).To(Equal(int32(20)))
			Expect(apiHPA.Annotations[managedHPAPolicyAnnotation]).To(Equal("api-only"))

			webHPA := fetchHPA(testCtx, testNs, "web")
			Expect(webHPA.Spec.MaxReplicas).To(Equal(int32(10)))
			Expect(webHPA.Annotations[managedHPAPolicyAnnotation]).To(Equal("wide"))
		})

		It("applies a selector policy only to Deployments whose pod template labels match", func() {
			selective := integPolicy(testNs, "selective", 0, true)
			selective.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}
			Expect(k8sClient.Create(testCtx, selective)).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "api", nil))).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "web", nil))).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, selective.Name)

			Expect(hpaExists(testCtx, testNs, "api")).To(BeTrue())
			Expect(hpaExists(testCtx, testNs, "web")).To(BeFalse(),
				"a Deployment outside the selector must not get an HPA from this policy")
		})

		It("leaves a Deployment ungoverned when the only policy targets another Deployment", func() {
			other := integPolicy(testNs, "other", 0, true)
			other.Spec.TargetRef = &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}
			Expect(k8sClient.Create(testCtx, other)).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "web", nil))).To(Succeed())

			reconcilePolicy(testCtx, reconciler, testNs, other.Name)

			Expect(hpaExists(testCtx, testNs, "web")).To(BeFalse())
		})
	})

	Context("Retargeting", func() {
		It("removes the HPA it created once the policy stops targeting the Deployment, leaving other HPAs alone", func() {
			policy := integPolicy(testNs, "moving", 0, true)
			policy.Spec.TargetRef = &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}
			Expect(k8sClient.Create(testCtx, policy)).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "api", nil))).To(Succeed())
			Expect(k8sClient.Create(testCtx, integDeployment(testNs, "web", nil))).To(Succeed())
			reconcilePolicy(testCtx, reconciler, testNs, policy.Name)
			Expect(hpaExists(testCtx, testNs, "api")).To(BeTrue())

			// An HPA that the operator does not manage must survive retargeting.
			foreign := &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: hpaName("web"), Namespace: testNs},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "web"},
					MaxReplicas:    3,
				},
			}
			Expect(k8sClient.Create(testCtx, foreign)).To(Succeed())

			key := types.NamespacedName{Name: policy.Name, Namespace: testNs}
			Expect(k8sClient.Get(testCtx, key, policy)).To(Succeed())
			policy.Spec.TargetRef.Name = "web"
			Expect(k8sClient.Update(testCtx, policy)).To(Succeed())

			// web is occupied by an unmanaged HPA, so reconcile reports it; api must still be released.
			_, _ = reconciler.Reconcile(testCtx, reconcile.Request{NamespacedName: key})

			Expect(hpaExists(testCtx, testNs, "api")).To(BeFalse(),
				"HPA of a Deployment no longer governed must be released")
			Expect(fetchHPA(testCtx, testNs, "web").Spec.MaxReplicas).To(Equal(int32(3)),
				"unmanaged HPA must not be touched")
		})
	})

	Context("API server contract", func() {
		It("rejects a targetRef without a name through the CRD schema", func() {
			policy := integPolicy(testNs, "bad-ref", 0, true)
			policy.Spec.TargetRef = &corev1alpha1.PolicyTargetReference{Kind: "Deployment"}

			err := k8sClient.Create(testCtx, policy)

			Expect(err).To(HaveOccurred())
		})

		It("persists targetRef and selector inline in the spec", func() {
			baseline := &corev1alpha1.SecurityBaseline{
				ObjectMeta: metav1.ObjectMeta{Name: "targeted", Namespace: testNs},
				Spec: corev1alpha1.SecurityBaselineSpec{
					RunAsNonRoot: true,
					PolicyTargeting: corev1alpha1.PolicyTargeting{
						TargetRef: &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "StatefulSet", Name: "db"},
						Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "data"}},
					},
				},
			}
			Expect(k8sClient.Create(testCtx, baseline)).To(Succeed())

			stored := &corev1alpha1.SecurityBaseline{}
			Expect(k8sClient.Get(testCtx, types.NamespacedName{Name: "targeted", Namespace: testNs}, stored)).To(Succeed())

			Expect(stored.Spec.TargetRef).NotTo(BeNil())
			Expect(stored.Spec.TargetRef.Name).To(Equal("db"))
			Expect(stored.Spec.Selector.MatchLabels).To(HaveKeyWithValue("tier", "data"))
		})
	})
})
