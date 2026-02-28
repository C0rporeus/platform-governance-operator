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
