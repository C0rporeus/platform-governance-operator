package core

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestPodMutatorApplyTelemetryUsesHighestPriorityFirst(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{
		Recorder: record.NewFakeRecorder(10),
	}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	profiles := []platformv1alpha1.TelemetryProfile{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "low"},
			Spec: platformv1alpha1.TelemetryProfileSpec{
				Priority:        1,
				InjectEnvVars:   true,
				TracingEndpoint: "http://low:4317",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "high"},
			Spec: platformv1alpha1.TelemetryProfileSpec{
				Priority:        10,
				InjectEnvVars:   true,
				TracingEndpoint: "http://high:4317",
			},
		},
	}

	sortTelemetryProfilesByPriority(profiles)
	mutated := mutator.applyTelemetry(pod, profiles)
	if !mutated {
		t.Fatalf("expected pod to be mutated")
	}

	endpoint := ""
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
			endpoint = env.Value
		}
	}
	if endpoint != "http://high:4317" {
		t.Fatalf("expected highest priority endpoint, got %q", endpoint)
	}
}

func TestPodMutatorApplyPolicyResourcesRejectsInvalidQuantity(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{
		Recorder: record.NewFakeRecorder(10),
	}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			DefaultRequests: map[string]string{"cpu": "not-a-quantity"},
		},
	}

	_, err := mutator.applyPolicyResources(pod, policy)
	if err == nil {
		t.Fatalf("expected invalid quantity error")
	}
}
