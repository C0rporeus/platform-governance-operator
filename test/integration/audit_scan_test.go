package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platform "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
)

func auditClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := platform.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func auditPod(namespace, name string, nonRoot bool) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace, Name: name, Labels: map[string]string{"app": name}},
		Spec: corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot},
			Containers: []corev1.Container{{Name: "app", Image: "example/app:v1"}}}}
}

func TestAuditAssessmentUsesGovernorAndPolicyScope(t *testing.T) {
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"},
		Spec: platform.SecurityBaselineSpec{RunAsNonRoot: true, EnforcementAction: platform.EnforcementActionAudit}}
	other := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "team"},
		Spec: platform.SecurityBaselineSpec{ReadOnlyRootFilesystem: true}}
	scanner := controller.AuditReconciler{Client: auditClient(t, baseline, other,
		auditPod("team", "bad", false), auditPod("team", "good", true), auditPod("outside", "ignored", false))}
	report, err := scanner.Assess(context.Background(), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if report.Evaluated != 2 || report.Violating != 1 || !report.Complete {
		t.Fatalf("unexpected report: %+v", report)
	}
	baseline.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "good"}}
	if err := scanner.Client.Update(context.Background(), baseline); err != nil {
		t.Fatal(err)
	}
	report, err = scanner.Assess(context.Background(), baseline)
	if err != nil || report.Evaluated != 1 || report.Violating != 0 {
		t.Fatalf("selector: %+v, %v", report, err)
	}
}

func TestAuditClusterNamespaceExclusionsAndBudget(t *testing.T) {
	baseline := &platform.ClusterSecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "global"},
		Spec: platform.ClusterSecurityBaselineSpec{SecurityBaselineSpec: platform.SecurityBaselineSpec{
			RunAsNonRoot: true, ExcludedNamespaces: []string{"except"}},
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"audit": "yes"}}}}
	objects := make([]client.Object, 0, 12)
	objects = append(objects, baseline)
	for _, namespace := range []string{"team", "kube-system", "operator", "except", "unselected"} {
		labels := map[string]string{"audit": "yes"}
		if namespace == "unselected" {
			labels = nil
		}
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: labels}},
			auditPod(namespace, "bad", false))
	}
	objects = append(objects, auditPod("team", "second", false))
	scanner := controller.AuditReconciler{Client: auditClient(t, objects...), OperatorNamespace: "operator", MaxPods: 1}
	report, err := scanner.Assess(context.Background(), baseline)
	if err != nil || report.Inspected != 1 || report.Evaluated != 1 || report.Violating != 1 || report.Complete {
		t.Fatalf("bounded report: %+v, %v", report, err)
	}
	scanner.MaxPods = 2
	report, err = scanner.Assess(context.Background(), baseline)
	if err != nil || report.Evaluated != 2 || !report.Complete {
		t.Fatalf("complete report: %+v, %v", report, err)
	}
}

func TestAuditNeverReportsReadFailureAsComplete(t *testing.T) {
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"}}
	base := auditClient(t, baseline, auditPod("team", "pod", false))
	scanner := controller.AuditReconciler{Client: interceptor.NewClient(base, interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errors.New("cache unavailable")
		},
	})}
	report, err := scanner.Assess(context.Background(), baseline)
	if err == nil || report.Complete {
		t.Fatalf("read failure: %+v, %v", report, err)
	}
}

func TestAuditTargetAndInvalidSelector(t *testing.T) {
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"},
		Spec: platform.SecurityBaselineSpec{RunAsNonRoot: true, PolicyTargeting: platform.PolicyTargeting{
			TargetRef: &platform.PolicyTargetReference{Kind: "Deployment", Group: "apps", Name: "web"}}}}
	pod := auditPod("team", "selected", false)
	pod.Labels["pod-template-hash"] = "abc"
	pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "web-abc"}}
	scanner := controller.AuditReconciler{Client: auditClient(t, baseline, pod, auditPod("team", "other", false))}
	report, err := scanner.Assess(context.Background(), baseline)
	if err != nil || report.Evaluated != 1 || report.Violating != 1 || !report.Complete {
		t.Fatalf("target report: %+v, %v", report, err)
	}
	baseline.Spec.TargetRef = nil
	baseline.Spec.Selector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}},
	}
	report, err = scanner.Assess(context.Background(), baseline)
	if err == nil || report.Complete {
		t.Fatalf("invalid selector: %+v, %v", report, err)
	}
}

func TestAuditBoundsExamplesWithoutUndercountingPods(t *testing.T) {
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"},
		Spec: platform.SecurityBaselineSpec{RunAsNonRoot: true, ReadOnlyRootFilesystem: true}}
	objects := make([]client.Object, 0, 13)
	objects = append(objects, baseline)
	for i := range 12 {
		objects = append(objects, auditPod("team", fmt.Sprintf("pod-%02d", i), false))
	}
	scanner := controller.AuditReconciler{Client: auditClient(t, objects...)}
	report, err := scanner.Assess(context.Background(), baseline)
	if err != nil || report.Violating != 12 || len(report.Examples) != controller.AuditExampleLimit {
		t.Fatalf("example budget or Pod count: %+v, %v", report, err)
	}
}

var _ = Describe("Periodic baseline assessment", func() {
	It("reassesses existing Pods after a policy edit and schedules another cycle", func() {
		baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "audit-periodic", Namespace: "default"},
			Spec: platform.SecurityBaselineSpec{EnforcementAction: platform.EnforcementActionAudit}}
		Expect(k8sClient.Create(ctx, baseline)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, baseline)).To(Succeed()) })
		pod := auditPod("default", "audit-existing-pod", false)
		readOnly := true
		pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, pod)).To(Succeed()) })
		scanner := controller.AuditReconciler{Client: k8sClient}
		before, err := scanner.Assess(ctx, baseline)
		Expect(err).NotTo(HaveOccurred())
		Expect(before.Violating).To(BeZero())
		r := &controller.SecurityBaselineReconciler{Client: k8sClient}
		request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(baseline)}
		result, err := r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(controller.AuditInterval))
		Expect(k8sClient.Get(ctx, request.NamespacedName, baseline)).To(Succeed())
		baseline.Spec.RunAsNonRoot = true
		Expect(k8sClient.Update(ctx, baseline)).To(Succeed())
		after, err := scanner.Assess(ctx, baseline)
		Expect(err).NotTo(HaveOccurred())
		Expect(after.Violating).To(BeNumerically(">=", 1))
		result, err = r.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(controller.AuditInterval))
		Expect(k8sClient.Get(ctx, request.NamespacedName, baseline)).To(Succeed())
		Expect(baseline.Status.Compliance).NotTo(BeNil())
		Expect(baseline.Status.Compliance.Complete).To(BeTrue())
		Expect(baseline.Status.Compliance.Violating).To(BeNumerically(">=", 1))
		Expect(baseline.Status.Compliance.Examples).NotTo(BeEmpty())
	})
})
