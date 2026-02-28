package core

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

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

func TestPodMutatorApplyLabelsInjectsAndDoesNotOverride(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{Recorder: record.NewFakeRecorder(10)}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"existing": "keep-me"},
		},
	}
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]string{
				"cost-center": "engineering",
				"existing":    "override-attempt",
			},
		},
	}

	mutated := mutator.applyPolicyLabels(pod, policy)
	if !mutated {
		t.Fatalf("expected labels to be mutated")
	}
	if pod.Labels["cost-center"] != "engineering" {
		t.Fatalf("expected cost-center label to be injected, got %q", pod.Labels["cost-center"])
	}
	if pod.Labels["existing"] != "keep-me" {
		t.Fatalf("expected existing label to be preserved, got %q", pod.Labels["existing"])
	}
}

func TestPodMutatorApplyLabelsNilLabelsMap(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{Recorder: record.NewFakeRecorder(10)}
	pod := &corev1.Pod{}
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]string{"team": "platform"},
		},
	}

	mutated := mutator.applyPolicyLabels(pod, policy)
	if !mutated {
		t.Fatalf("expected labels to be mutated")
	}
	if pod.Labels["team"] != "platform" {
		t.Fatalf("expected team label, got %q", pod.Labels["team"])
	}
}

func TestPodMutatorApplyResourcesHappyPath(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{Recorder: record.NewFakeRecorder(10)}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "sidecar"},
			},
		},
	}
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			DefaultRequests: map[string]string{"cpu": "100m", "memory": "128Mi"},
			DefaultLimits:   map[string]string{"cpu": "500m", "memory": "512Mi"},
		},
	}

	mutated, err := mutator.applyPolicyResources(pod, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mutated {
		t.Fatalf("expected resources to be mutated")
	}

	// Verify both containers got resources
	for _, c := range pod.Spec.Containers {
		cpuReq := c.Resources.Requests[corev1.ResourceCPU]
		if cpuReq.Cmp(resource.MustParse("100m")) != 0 {
			t.Fatalf("container %s: expected cpu request 100m, got %s", c.Name, cpuReq.String())
		}
		memLimit := c.Resources.Limits[corev1.ResourceMemory]
		if memLimit.Cmp(resource.MustParse("512Mi")) != 0 {
			t.Fatalf("container %s: expected memory limit 512Mi, got %s", c.Name, memLimit.String())
		}
	}
}

func TestPodMutatorApplyResourcesDoesNotOverrideExisting(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{Recorder: record.NewFakeRecorder(10)}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("200m"),
						},
					},
				},
			},
		},
	}
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "policy"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			DefaultRequests: map[string]string{"cpu": "100m", "memory": "128Mi"},
		},
	}

	mutated, err := mutator.applyPolicyResources(pod, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mutated {
		t.Fatalf("expected mutation (memory was added)")
	}

	// CPU should remain 200m (not overridden to 100m)
	cpuReq := pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
	if cpuReq.Cmp(resource.MustParse("200m")) != 0 {
		t.Fatalf("expected existing cpu request 200m preserved, got %s", cpuReq.String())
	}
	// Memory should be injected
	memReq := pod.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
	if memReq.Cmp(resource.MustParse("128Mi")) != 0 {
		t.Fatalf("expected memory request 128Mi injected, got %s", memReq.String())
	}
}

func TestPodMutatorHandleFullFlow(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: "team-a"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]string{"env": "production"},
			DefaultRequests: map[string]string{"cpu": "100m"},
		},
	}
	profile := &platformv1alpha1.TelemetryProfile{
		ObjectMeta: metav1.ObjectMeta{Name: "tracing", Namespace: "team-a"},
		Spec: platformv1alpha1.TelemetryProfileSpec{
			InjectEnvVars:   true,
			TracingEndpoint: "http://otel:4317",
			SamplingRate:    "0.5",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy, profile).Build()
	mutator := &PodMutator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := mutator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod to be allowed (mutating webhook), got denied")
	}
	if len(resp.Patches) == 0 {
		t.Fatalf("expected JSON patches to be applied")
	}
}

func TestPodMutatorHandleNoMutationsNeeded(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	// No policies or profiles in namespace
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	mutator := &PodMutator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := mutator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod to be allowed when no policies exist")
	}
	if len(resp.Patches) != 0 {
		t.Fatalf("expected no patches when no mutations needed, got %d", len(resp.Patches))
	}
}

func TestPodMutatorHandleSkipsInvalidPolicyAndAppliesValid(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	badPolicy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-policy", Namespace: "team-a"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			Priority:        10,
			DefaultRequests: map[string]string{"cpu": "not-a-quantity"},
		},
	}
	goodPolicy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "good-policy", Namespace: "team-a"},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			Priority:        1,
			MandatoryLabels: map[string]string{"team": "platform"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(badPolicy, goodPolicy).Build()
	mutator := &PodMutator{
		Client:   cl,
		Recorder: record.NewFakeRecorder(10),
		decoder:  admission.NewDecoder(scheme),
	}

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	resp := mutator.Handle(context.Background(), newAdmissionRequest(t, "team-a", pod))
	if !resp.Allowed {
		t.Fatalf("expected pod to be allowed even when one policy has invalid config")
	}
	if len(resp.Patches) == 0 {
		t.Fatalf("expected patches from the good policy")
	}
}

func TestPodMutatorTelemetryDoesNotOverrideExistingEnvVars(t *testing.T) {
	t.Parallel()

	mutator := &PodMutator{Recorder: record.NewFakeRecorder(10)}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Env: []corev1.EnvVar{
						{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://custom:4317"},
					},
				},
			},
		},
	}
	profiles := []platformv1alpha1.TelemetryProfile{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "profile"},
			Spec: platformv1alpha1.TelemetryProfileSpec{
				Priority:        10,
				InjectEnvVars:   true,
				TracingEndpoint: "http://override-attempt:4317",
			},
		},
	}

	mutated := mutator.applyTelemetry(pod, profiles)
	if mutated {
		t.Fatalf("expected no mutation when env var already exists")
	}
	endpoint := pod.Spec.Containers[0].Env[0].Value
	if endpoint != "http://custom:4317" {
		t.Fatalf("expected existing endpoint preserved, got %q", endpoint)
	}
}

func TestSortWorkloadPoliciesByPriority(t *testing.T) {
	t.Parallel()

	policies := []platformv1alpha1.WorkloadPolicy{
		{ObjectMeta: metav1.ObjectMeta{Name: "low"}, Spec: platformv1alpha1.WorkloadPolicySpec{Priority: 1}},
		{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Spec: platformv1alpha1.WorkloadPolicySpec{Priority: 10}},
		{ObjectMeta: metav1.ObjectMeta{Name: "mid"}, Spec: platformv1alpha1.WorkloadPolicySpec{Priority: 5}},
	}

	sortWorkloadPoliciesByPriority(policies)

	if policies[0].Name != "high" || policies[1].Name != "mid" || policies[2].Name != "low" {
		t.Fatalf("expected descending priority order, got %s, %s, %s",
			policies[0].Name, policies[1].Name, policies[2].Name)
	}
}
