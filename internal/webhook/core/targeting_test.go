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

package core

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// targetedPod returns a Pod owned by ReplicaSet "api-7d9f8" (Deployment "api")
// that violates runAsNonRoot and carries the label app=api.
func targetedPod() *corev1.Pod {
	isController := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "api-7d9f8-abcde",
			Labels: map[string]string{"app": "api", "pod-template-hash": "7d9f8"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f8", Controller: &isController,
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
}

func baselineWithTargeting(name string, targeting platformv1alpha1.PolicyTargeting) *platformv1alpha1.SecurityBaseline {
	return &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			RunAsNonRoot:    true,
			PolicyTargeting: targeting,
		},
	}
}

func TestPodValidatorHonorsTargeting(t *testing.T) {
	t.Parallel()

	selectorMatch := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}
	selectorMiss := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	deployRef := &platformv1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}
	otherRef := &platformv1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "web"}

	tests := []struct {
		name      string
		targeting platformv1alpha1.PolicyTargeting
		wantAllow bool
	}{
		{"no targeting applies to namespace", platformv1alpha1.PolicyTargeting{}, false},
		{"matching selector applies", platformv1alpha1.PolicyTargeting{Selector: selectorMatch}, false},
		{"non-matching selector does not apply", platformv1alpha1.PolicyTargeting{Selector: selectorMiss}, true},
		{"targetRef to owning Deployment applies", platformv1alpha1.PolicyTargeting{TargetRef: deployRef}, false},
		{"targetRef to other Deployment does not apply", platformv1alpha1.PolicyTargeting{TargetRef: otherRef}, true},
		{"targetRef wins over matching selector", platformv1alpha1.PolicyTargeting{TargetRef: otherRef, Selector: selectorMatch}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scheme := newWebhookTestScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baselineWithTargeting("b", tt.targeting)).Build()
			validator := &PodValidator{Client: cl, Recorder: record.NewFakeRecorder(10), decoder: admission.NewDecoder(scheme)}

			resp := validator.Handle(context.Background(), newAdmissionRequest(t, targetedPod()))
			if resp.Allowed != tt.wantAllow {
				t.Fatalf("allowed = %v, want %v (%v)", resp.Allowed, tt.wantAllow, resp.Result)
			}
		})
	}
}

func TestPodValidatorSkipsBaselineWithInvalidSelector(t *testing.T) {
	t.Parallel()

	bad := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Bogus"}}}
	scheme := newWebhookTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(baselineWithTargeting("bad", platformv1alpha1.PolicyTargeting{Selector: bad})).Build()
	validator := &PodValidator{Client: cl, Recorder: record.NewFakeRecorder(10), decoder: admission.NewDecoder(scheme)}

	resp := validator.Handle(context.Background(), newAdmissionRequest(t, targetedPod()))
	if !resp.Allowed {
		t.Fatalf("expected invalid-selector baseline to be skipped, got %v", resp.Result)
	}
}

func TestPodMutatorMoreSpecificPolicyWinsOverHigherPriority(t *testing.T) {
	t.Parallel()

	scheme := newWebhookTestScheme(t)
	namespaceWide := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "ns-wide", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			Priority:        100,
			DefaultRequests: map[string]string{"cpu": "100m"},
		},
	}
	targeted := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "api-only", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			DefaultRequests: map[string]string{"cpu": "500m"},
			PolicyTargeting: platformv1alpha1.PolicyTargeting{
				TargetRef: &platformv1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"},
			},
		},
	}
	unrelated := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "web-only", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			DefaultRequests: map[string]string{"memory": "1Gi"},
			PolicyTargeting: platformv1alpha1.PolicyTargeting{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespaceWide, targeted, unrelated).Build()
	mutator := &PodMutator{Client: cl, Recorder: record.NewFakeRecorder(10), decoder: admission.NewDecoder(scheme)}

	pod := targetedPod()
	resp := mutator.Handle(context.Background(), newAdmissionRequest(t, pod))
	if !resp.Allowed || len(resp.Patches) == 0 {
		t.Fatalf("expected a mutation patch, got %+v", resp)
	}

	var cpu, memory bool
	for _, patch := range resp.Patches {
		raw := patch.Json()
		if strings.Contains(raw, "500m") {
			cpu = true
		}
		if strings.Contains(raw, "1Gi") {
			memory = true
		}
		if strings.Contains(raw, "100m") {
			t.Fatalf("namespace-wide default must not override the targeted one: %s", raw)
		}
	}
	if !cpu {
		t.Fatalf("expected cpu=500m from the targetRef policy, patches: %+v", resp.Patches)
	}
	if memory {
		t.Fatalf("policy selecting app=web must not apply to app=api pod")
	}
}
