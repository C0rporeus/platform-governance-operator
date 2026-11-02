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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestTargetingWarnings(t *testing.T) {
	ref := func(group, kind string) corev1alpha1.PolicyTargeting {
		return corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Group: group, Kind: kind, Name: "x"}}
	}
	selector := func(labels map[string]string, expressions ...metav1.LabelSelectorRequirement) corev1alpha1.PolicyTargeting {
		return corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{MatchLabels: labels, MatchExpressions: expressions}}
	}

	cases := []struct {
		name    string
		in      corev1alpha1.PolicyTargeting
		want    int
		mention string
	}{
		{"no targeting", corev1alpha1.PolicyTargeting{}, 0, ""},
		{"targetRef to a Deployment", ref("apps", "Deployment"), 0, ""},
		{"targetRef to a StatefulSet", ref("apps", "StatefulSet"), 0, ""},
		{"targetRef to a Pod", ref("", "Pod"), 1, "Pod names"},
		{"targetRef to a ReplicaSet", ref("apps", "ReplicaSet"), 1, "ReplicaSet hashes"},
		{"selector on ordinary labels", selector(map[string]string{"app": "web", "tier": "data"}), 0, ""},
		{"selector on pod-template-hash", selector(map[string]string{"pod-template-hash": "abc"}), 1, "pod-template-hash"},
		{"controller label in an expression", selector(nil, metav1.LabelSelectorRequirement{
			Key: "job-name", Operator: metav1.LabelSelectorOpExists}), 1, "job-name"},
		{"several controller labels are listed once, sorted", selector(
			map[string]string{"pod-template-hash": "a", "controller-revision-hash": "b"},
			metav1.LabelSelectorRequirement{Key: "pod-template-hash", Operator: metav1.LabelSelectorOpExists}),
			1, "controller-revision-hash, pod-template-hash"},
	}
	for _, c := range cases {
		got := TargetingWarnings(c.in)
		if len(got) != c.want {
			t.Errorf("%s: got %d warnings %v, want %d", c.name, len(got), got, c.want)
			continue
		}
		if c.mention != "" && !strings.Contains(got[0], c.mention) {
			t.Errorf("%s: warning %q does not mention %q", c.name, got[0], c.mention)
		}
	}
}

func TestTargetingWarningsCoverTargetRefAndSelectorTogether(t *testing.T) {
	got := TargetingWarnings(corev1alpha1.PolicyTargeting{
		TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Pod", Name: "x"},
		Selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"controller-uid": "u"}},
	})
	if len(got) != 2 {
		t.Fatalf("expected one warning for each part of the targeting, got %v", got)
	}
}
