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
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
	corewebhook "github.com/f3nr1r/platform-governance-operator/internal/webhook/core"
)

var _ = Describe("ClusterSecurityBaseline Controller", controllerSmokeBody(controllerSmokeSpec{
	Kind:         "ClusterSecurityBaseline",
	ResourceName: "test-clustersecuritybaseline",
	NewObject:    func() client.Object { return &corev1alpha1.ClusterSecurityBaseline{} },
	NewReconciler: func() reconcile.Reconciler {
		return &controller.ClusterSecurityBaselineReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
		}
	},
	Conditions: func(o client.Object) []metav1.Condition {
		return o.(*corev1alpha1.ClusterSecurityBaseline).Status.Conditions
	},
}))

var _ = Describe("ClusterSecurityBaseline inheritance", func() {
	const operatorNamespace = "pgo-operator"

	var (
		testCtx   context.Context
		seq       string
		validator *corewebhook.PodValidator
	)

	newNamespace := func(name string, labels map[string]string) string {
		GinkgoHelper()
		full := fmt.Sprintf("%s-%s", name, seq)
		Expect(k8sClient.Create(testCtx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: full, Labels: labels},
		})).To(Succeed())
		return full
	}

	admit := func(namespace string) admission.Response {
		GinkgoHelper()
		raw, err := json.Marshal(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1"}}},
		})
		Expect(err).NotTo(HaveOccurred())
		return validator.Handle(testCtx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: namespace,
			Object:    runtime.RawExtension{Raw: raw},
		}})
	}

	BeforeEach(func() {
		hpaIntegSeq++
		seq = fmt.Sprintf("%04d", hpaIntegSeq)
		testCtx = context.Background()

		validator = &corewebhook.PodValidator{
			Client:            k8sClient,
			Recorder:          record.NewFakeRecorder(50),
			OperatorNamespace: operatorNamespace,
		}
		Expect(validator.InjectDecoder(admission.NewDecoder(k8sClient.Scheme()))).To(Succeed())
	})

	AfterEach(func() {
		_ = k8sClient.DeleteAllOf(testCtx, &corev1alpha1.ClusterSecurityBaseline{})
	})

	It("denies Pods in namespaces that have no baseline of their own", func() {
		Expect(k8sClient.Create(testCtx, &corev1alpha1.ClusterSecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-" + seq},
			Spec: corev1alpha1.ClusterSecurityBaselineSpec{
				SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
			},
		})).To(Succeed())
		ns := newNamespace("fresh", nil)

		resp := admit(ns)

		Expect(resp.Allowed).To(BeFalse())
		Expect(resp.Result.Message).To(ContainSubstring("cluster/corp-" + seq))
	})

	It("cannot be weakened by an Audit baseline in the namespace", func() {
		Expect(k8sClient.Create(testCtx, &corev1alpha1.ClusterSecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-" + seq},
			Spec: corev1alpha1.ClusterSecurityBaselineSpec{
				SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
			},
		})).To(Succeed())
		ns := newNamespace("lenient", nil)
		Expect(k8sClient.Create(testCtx, &corev1alpha1.SecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "audit-only", Namespace: ns},
			Spec: corev1alpha1.SecurityBaselineSpec{
				EnforcementAction: corev1alpha1.EnforcementActionAudit,
				RunAsNonRoot:      true,
			},
		})).To(Succeed())

		Expect(admit(ns).Allowed).To(BeFalse())
	})

	It("skips system namespaces, the operator namespace and explicit exclusions", func() {
		Expect(k8sClient.Create(testCtx, &corev1alpha1.ClusterSecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-" + seq},
			Spec: corev1alpha1.ClusterSecurityBaselineSpec{
				SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
			},
		})).To(Succeed())

		Expect(admit("kube-system").Allowed).To(BeTrue())
		Expect(admit("kube-public").Allowed).To(BeTrue())
		Expect(admit("cert-manager").Allowed).To(BeTrue())
		Expect(admit(operatorNamespace).Allowed).To(BeTrue())
	})

	It("honors namespaceSelector against real Namespace labels", func() {
		Expect(k8sClient.Create(testCtx, &corev1alpha1.ClusterSecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "prod-only-" + seq},
			Spec: corev1alpha1.ClusterSecurityBaselineSpec{
				SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
				NamespaceSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			},
		})).To(Succeed())
		prod := newNamespace("prod", map[string]string{"env": "prod"})
		dev := newNamespace("dev", map[string]string{"env": "dev"})

		Expect(admit(prod).Allowed).To(BeFalse())
		Expect(admit(dev).Allowed).To(BeTrue())
	})

	It("rejects a targetRef through the CRD schema (CEL)", func() {
		err := k8sClient.Create(testCtx, &corev1alpha1.ClusterSecurityBaseline{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-" + seq},
			Spec: corev1alpha1.ClusterSecurityBaselineSpec{
				SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{
					RunAsNonRoot: true,
					PolicyTargeting: corev1alpha1.PolicyTargeting{
						TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Deployment", Name: "x"},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("targetRef is not supported"))
	})
})
