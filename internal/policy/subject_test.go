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

package policy

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func hasRef(s Subject, ref corev1alpha1.PolicyTargetReference) bool {
	return containsRef(s.Refs, ref)
}

func TestSubjectForPodDerivesDeploymentFromReplicaSet(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:            "api-7d9f8-abcde",
		Labels:          map[string]string{"pod-template-hash": "7d9f8", "app": "api"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f8"}},
	}}
	s := SubjectForPod(pod)
	for _, ref := range []corev1alpha1.PolicyTargetReference{
		{Kind: "Pod", Name: "api-7d9f8-abcde"},
		{Group: "apps", Kind: "ReplicaSet", Name: "api-7d9f8"},
		{Group: "apps", Kind: "Deployment", Name: "api"},
	} {
		if !hasRef(s, ref) {
			t.Fatalf("missing ref %+v in %+v", ref, s.Refs)
		}
	}
	if s.Labels["app"] != "api" {
		t.Fatalf("labels not propagated: %v", s.Labels)
	}
}

func TestSubjectForPodDoesNotGuessDeployment(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		owner  metav1.OwnerReference
	}{
		{"missing hash label", nil, metav1.OwnerReference{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f8"}},
		{"hash does not match suffix", map[string]string{"pod-template-hash": "other"},
			metav1.OwnerReference{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "api-7d9f8"}},
		{"replicaset name equals hash", map[string]string{"pod-template-hash": "abc"},
			metav1.OwnerReference{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "-abc"}},
		{"non-apps ReplicaSet", map[string]string{"pod-template-hash": "7d9f8"},
			metav1.OwnerReference{APIVersion: "example.io/v1", Kind: "ReplicaSet", Name: "api-7d9f8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: tt.labels, OwnerReferences: []metav1.OwnerReference{tt.owner}}}
			for _, ref := range SubjectForPod(pod).Refs {
				if ref.Kind == "Deployment" {
					t.Fatalf("unexpected Deployment ref %+v", ref)
				}
			}
		})
	}
}

func TestSubjectForPodDirectOwnersAndBadAPIVersion(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "db"},
		{APIVersion: "batch/v1", Kind: "Job", Name: "migrate"},
		{APIVersion: "///bad", Kind: "Thing", Name: "x"},
	}}}
	s := SubjectForPod(pod)
	if !hasRef(s, corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "StatefulSet", Name: "db"}) ||
		!hasRef(s, corev1alpha1.PolicyTargetReference{Group: "batch", Kind: "Job", Name: "migrate"}) {
		t.Fatalf("missing direct owners: %+v", s.Refs)
	}
	if len(s.Refs) != 2 {
		t.Fatalf("pod without name and bad apiVersion should add no refs, got %+v", s.Refs)
	}
}

func TestSubjectForDeploymentUsesTemplateLabels(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Labels: map[string]string{"meta": "x"}}}
	d.Spec.Template.Labels = map[string]string{"app": "api"}
	s := SubjectForDeployment(d)
	if s.Labels["app"] != "api" || s.Labels["meta"] != "" {
		t.Fatalf("expected template labels, got %v", s.Labels)
	}
	if !hasRef(s, corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}) {
		t.Fatalf("missing deployment ref: %+v", s.Refs)
	}
}

func TestSelectPreservesItemsAndOrder(t *testing.T) {
	type item struct {
		name string
		prio int32
		t    corev1alpha1.PolicyTargeting
	}
	items := []item{
		{"low", 1, corev1alpha1.PolicyTargeting{}},
		{"targeted", 0, corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef}},
		{"high", 9, corev1alpha1.PolicyTargeting{}},
		{"miss", 0, corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "web")}},
	}
	got, errs := Select(items, Subject{Refs: []corev1alpha1.PolicyTargetReference{deploymentRef}, Labels: map[string]string{"app": "api"}},
		func(i item) Candidate { return Candidate{Name: i.name, Priority: i.prio, Targeting: i.t} })
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want := []string{"targeted", "high", "low"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].name != want[i] {
			t.Fatalf("position %d: got %s want %s", i, got[i].name, want[i])
		}
	}
}

func TestValidateTargeting(t *testing.T) {
	bad := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "a", Operator: "Bogus"}}}
	tests := []struct {
		name    string
		in      corev1alpha1.PolicyTargeting
		wantErr bool
	}{
		{"empty", corev1alpha1.PolicyTargeting{}, false},
		{"valid both", corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef, Selector: selectorFor("a", "b")}, false},
		{"missing name", corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Deployment"}}, true},
		{"missing kind", corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Name: "x"}}, true},
		{"invalid selector", corev1alpha1.PolicyTargeting{Selector: bad}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTargeting(tt.in); (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
