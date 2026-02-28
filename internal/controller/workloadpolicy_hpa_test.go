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

package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestHPAEnabledForDeploymentWithAnnotationOverride(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app",
			Namespace:   "default",
			Annotations: map[string]string{deploymentHPAEnabledAnnotation: "true"},
		},
	}

	enabled, err := hpaEnabledForDeployment(deployment, &corev1alpha1.HorizontalScalingPolicy{
		EnabledByDefault: false,
		MinReplicas:      2,
		MaxReplicas:      10,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !enabled {
		t.Fatalf("expected hpa to be enabled by annotation override")
	}
}

func TestHPAEnabledForDeploymentRejectsInvalidAnnotation(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app",
			Namespace:   "default",
			Annotations: map[string]string{deploymentHPAEnabledAnnotation: "maybe"},
		},
	}

	_, err := hpaEnabledForDeployment(deployment, &corev1alpha1.HorizontalScalingPolicy{
		EnabledByDefault: true,
		MinReplicas:      2,
		MaxReplicas:      10,
	})
	if err == nil {
		t.Fatalf("expected error for invalid annotation value")
	}
}

func TestDesiredHPAForDeploymentUsesPolicyValues(t *testing.T) {
	t.Parallel()

	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "default",
		},
		Spec: corev1alpha1.WorkloadPolicySpec{
			HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{
				EnabledByDefault:               true,
				MinReplicas:                    3,
				MaxReplicas:                    12,
				TargetCPUUtilizationPercentage: 65,
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "default",
		},
	}

	hpa := desiredHPAForDeployment(policy, deployment)
	if hpa.Name != "api-pgo-hpa" {
		t.Fatalf("expected managed hpa name, got %q", hpa.Name)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 3 {
		t.Fatalf("expected minReplicas 3")
	}
	if hpa.Spec.MaxReplicas != 12 {
		t.Fatalf("expected maxReplicas 12, got %d", hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Resource == nil {
		t.Fatalf("expected a CPU utilization metric")
	}
	if hpa.Spec.Metrics[0].Resource.Target.AverageUtilization == nil || *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 65 {
		t.Fatalf("expected target CPU utilization 65")
	}
}

func TestDesiredHPAForDeploymentHasOwnerReference(t *testing.T) {
	t.Parallel()

	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "default",
			UID:       "abc-123",
		},
		Spec: corev1alpha1.WorkloadPolicySpec{
			HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{
				MinReplicas: 2,
				MaxReplicas: 5,
			},
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
	}

	hpa := desiredHPAForDeployment(policy, deployment)
	if len(hpa.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(hpa.OwnerReferences))
	}
	ref := hpa.OwnerReferences[0]
	if ref.Name != "policy" {
		t.Fatalf("expected owner name 'policy', got %q", ref.Name)
	}
	if ref.Kind != "WorkloadPolicy" {
		t.Fatalf("expected owner kind 'WorkloadPolicy', got %q", ref.Kind)
	}
	if ref.UID != "abc-123" {
		t.Fatalf("expected owner UID 'abc-123', got %q", ref.UID)
	}
}

func TestIsManagedHPA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hpa      *autoscalingv2.HorizontalPodAutoscaler
		expected bool
	}{
		{
			name:     "nil HPA",
			hpa:      nil,
			expected: false,
		},
		{
			name: "no labels",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expected: false,
		},
		{
			name: "wrong label value",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test",
					Labels: map[string]string{managedHPALabelKey: "false"},
				},
			},
			expected: false,
		},
		{
			name: "managed HPA",
			hpa: &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test",
					Labels: map[string]string{managedHPALabelKey: managedHPALabelValue},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isManagedHPA(tt.hpa); got != tt.expected {
				t.Fatalf("isManagedHPA() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestEnsureManagedHPAMetadata(t *testing.T) {
	t.Parallel()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	ensureManagedHPAMetadata(hpa, "my-policy")

	if hpa.Labels[managedHPALabelKey] != managedHPALabelValue {
		t.Fatalf("expected managed label to be set")
	}
	if hpa.Annotations[managedHPAWorkloadPolicyAnnotationKey] != "my-policy" {
		t.Fatalf("expected policy annotation to be set")
	}
}

func TestManagedHPANameTruncation(t *testing.T) {
	t.Parallel()

	shortName := "api"
	if got := managedHPAName(shortName); got != "api-pgo-hpa" {
		t.Fatalf("expected 'api-pgo-hpa', got %q", got)
	}

	// 63 - len("-pgo-hpa") = 55 max base length
	longName := "a]bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	result := managedHPAName(longName)
	if len(result) > 63 {
		t.Fatalf("HPA name exceeds 63 chars: %d", len(result))
	}
}

func TestEffectiveHorizontalScalingPolicyDefaults(t *testing.T) {
	t.Parallel()

	// nil policy should return all defaults
	effective := effectiveHorizontalScalingPolicy(nil)
	if effective.MinReplicas != corev1alpha1.DefaultHPAMinReplicas {
		t.Fatalf("expected default MinReplicas %d, got %d", corev1alpha1.DefaultHPAMinReplicas, effective.MinReplicas)
	}
	if effective.MaxReplicas != corev1alpha1.DefaultHPAMaxReplicas {
		t.Fatalf("expected default MaxReplicas %d, got %d", corev1alpha1.DefaultHPAMaxReplicas, effective.MaxReplicas)
	}
	if effective.TargetCPUUtilizationPercentage != corev1alpha1.DefaultHPATargetCPU {
		t.Fatalf("expected default TargetCPU %d, got %d", corev1alpha1.DefaultHPATargetCPU, effective.TargetCPUUtilizationPercentage)
	}
}

func TestEffectiveHorizontalScalingPolicyPartialOverride(t *testing.T) {
	t.Parallel()

	// Only MinReplicas set, others should default
	policy := &corev1alpha1.HorizontalScalingPolicy{MinReplicas: 5}
	effective := effectiveHorizontalScalingPolicy(policy)
	if effective.MinReplicas != 5 {
		t.Fatalf("expected MinReplicas 5, got %d", effective.MinReplicas)
	}
	if effective.MaxReplicas != corev1alpha1.DefaultHPAMaxReplicas {
		t.Fatalf("expected default MaxReplicas, got %d", effective.MaxReplicas)
	}
	if effective.TargetCPUUtilizationPercentage != corev1alpha1.DefaultHPATargetCPU {
		t.Fatalf("expected default TargetCPU, got %d", effective.TargetCPUUtilizationPercentage)
	}
}

func TestHPAEnabledForDeploymentUsesDefault(t *testing.T) {
	t.Parallel()

	// No annotation -> uses EnabledByDefault
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}
	enabled, err := hpaEnabledForDeployment(deployment, &corev1alpha1.HorizontalScalingPolicy{
		EnabledByDefault: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected HPA enabled by default")
	}
}

func TestHPAEnabledForDeploymentAnnotationDisables(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app",
			Namespace:   "default",
			Annotations: map[string]string{deploymentHPAEnabledAnnotation: "false"},
		},
	}
	enabled, err := hpaEnabledForDeployment(deployment, &corev1alpha1.HorizontalScalingPolicy{
		EnabledByDefault: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatalf("expected HPA disabled by annotation override")
	}
}

func TestHPASpecDrifted(t *testing.T) {
	t.Parallel()

	minReplicas := int32(2)
	targetCPU := int32(80)

	base := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "api",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
	}

	// Identical -> no drift
	identical := base.DeepCopy()
	if hpaSpecDrifted(base, identical) {
		t.Fatalf("expected no drift for identical specs")
	}

	// Different MaxReplicas
	diffMax := base.DeepCopy()
	diffMax.Spec.MaxReplicas = 20
	if !hpaSpecDrifted(base, diffMax) {
		t.Fatalf("expected drift for different MaxReplicas")
	}

	// Different MinReplicas
	diffMin := base.DeepCopy()
	newMin := int32(5)
	diffMin.Spec.MinReplicas = &newMin
	if !hpaSpecDrifted(base, diffMin) {
		t.Fatalf("expected drift for different MinReplicas")
	}

	// Different CPU target
	diffCPU := base.DeepCopy()
	newCPU := int32(50)
	diffCPU.Spec.Metrics[0].Resource.Target.AverageUtilization = &newCPU
	if !hpaSpecDrifted(base, diffCPU) {
		t.Fatalf("expected drift for different CPU target")
	}

	// Different ScaleTargetRef
	diffRef := base.DeepCopy()
	diffRef.Spec.ScaleTargetRef.Name = "other-api"
	if !hpaSpecDrifted(base, diffRef) {
		t.Fatalf("expected drift for different ScaleTargetRef")
	}
}
