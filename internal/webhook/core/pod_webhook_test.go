package core

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestPodValidatorDeniesRootPod(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod admission to be denied")
	}
}

func TestPodValidatorAllowsExcludedNamespace(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot:       true,
			ExcludedNamespaces: []string{"team-a"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod admission to be allowed for excluded namespace")
	}
}

func TestPodValidatorDeniesReadWriteRootFilesystem(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied: container without readOnlyRootFilesystem")
	}
}

func TestPodValidatorDeniesInitContainerWithoutReadOnly(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	readOnly := true
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "app",
					SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly},
				},
			},
			InitContainers: []corev1.Container{
				{Name: "init"},
			},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied: init container without readOnlyRootFilesystem")
	}
}

func TestPodValidatorAllowsCompliantPod(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	runAsNonRoot := true
	readOnly := true
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot:           true,
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
			Containers: []corev1.Container{
				{
					Name:            "app",
					SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly},
				},
			},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected compliant pod to be allowed, got denied: %s", resp.Result.Message)
	}
}

func TestPodValidatorMultipleBaselines(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	runAsNonRoot := true
	// First baseline only requires runAsNonRoot, second requires readOnly
	baseline1 := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline-1", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot: true,
		},
	}
	baseline2 := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline-2", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline1, baseline2).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	// Pod has runAsNonRoot but no readOnly -> should be denied by baseline-2
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
			Containers:      []corev1.Container{{Name: "app"}},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied by second baseline (readOnlyRootFilesystem)")
	}
}

func TestPodValidatorAllowsWhenNoBaselinesExist(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod to be allowed when no baselines exist")
	}
}

func TestPodValidatorNilPodSecurityContextDenied(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	// Pod has no SecurityContext at all → must be denied
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			// SecurityContext is nil
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied: nil pod SecurityContext when RunAsNonRoot required")
	}
}

func TestPodValidatorNilContainerSecurityContextDenied(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	// Container has nil SecurityContext → must be denied
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", SecurityContext: nil},
			},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied: nil container SecurityContext when ReadOnlyRootFilesystem required")
	}
}

func TestPodValidatorNoContainersReadOnlyBaselineAllowed(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			ReadOnlyRootFilesystem: true,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	// Pod with no containers or init containers: the ReadOnly loops never execute → allowed
	pod := &corev1.Pod{}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod with no containers to be allowed (vacuously true for ReadOnlyRootFilesystem): %s", resp.Result.Message)
	}
}

func TestPodValidatorMultipleBaselinesAllSatisfied(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	runAsNonRoot := true
	readOnly := true

	baseline1 := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline-nonroot", Namespace: "team-a"},
		Spec:       platformv1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
	}
	baseline2 := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline-readonly", Namespace: "team-a"},
		Spec:       platformv1alpha1.SecurityBaselineSpec{ReadOnlyRootFilesystem: true},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline1, baseline2).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	// Pod satisfies both baselines → allowed
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot},
			Containers: []corev1.Container{
				{
					Name:            "app",
					SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly},
				},
			},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod satisfying all baselines to be allowed: %s", resp.Result.Message)
	}
}

func TestPodValidatorMultipleInitContainersOneViolates(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	readOnly := true
	baseline := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: "team-a"},
		Spec:       platformv1alpha1.SecurityBaselineSpec{ReadOnlyRootFilesystem: true},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseline).Build()
	validator := &PodValidator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "app",
					SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly},
				},
			},
			InitContainers: []corev1.Container{
				// init-compliant has readOnly=true
				{
					Name:            "init-compliant",
					SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly},
				},
				// init-violator has no SecurityContext → should cause denial
				{Name: "init-violator"},
			},
		},
	}
	resp := validator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if resp.Allowed {
		t.Fatalf("expected pod to be denied: second init container violates ReadOnlyRootFilesystem")
	}
}

func newWebhookTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add platformv1alpha1 to scheme: %v", err)
	}
	return scheme
}

func newAdmissionRequest(t *testing.T, namespace string, pod *corev1.Pod) admission.Request {
	t.Helper()

	rawPod, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("failed to marshal pod: %v", err)
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: namespace,
			Object: runtime.RawExtension{
				Raw: rawPod,
			},
		},
	}
}
