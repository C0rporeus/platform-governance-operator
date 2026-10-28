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

package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestClusterSecurityBaselineValidator(t *testing.T) {
	t.Parallel()

	valid := func() *corev1alpha1.ClusterSecurityBaseline {
		return &corev1alpha1.ClusterSecurityBaseline{Spec: corev1alpha1.ClusterSecurityBaselineSpec{
			SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
			NamespaceSelector:    &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
		}}
	}
	tests := []struct {
		name    string
		mutate  func(*corev1alpha1.ClusterSecurityBaseline)
		wantErr bool
	}{
		{"valid", func(*corev1alpha1.ClusterSecurityBaseline) {}, false},
		{"targetRef rejected", func(c *corev1alpha1.ClusterSecurityBaseline) {
			c.Spec.TargetRef = &corev1alpha1.PolicyTargetReference{Kind: "Deployment", Name: "x"}
		}, true},
		{"invalid namespaceSelector", func(c *corev1alpha1.ClusterSecurityBaseline) {
			c.Spec.NamespaceSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Bogus"}},
			}
		}, true},
		{"invalid pod selector", func(c *corev1alpha1.ClusterSecurityBaseline) {
			c.Spec.Selector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "a", Operator: "Bogus"}},
			}
		}, true},
		{"empty excludedNamespaces entry", func(c *corev1alpha1.ClusterSecurityBaseline) {
			c.Spec.ExcludedNamespaces = []string{" "}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := &ClusterSecurityBaselineCustomValidator{}
			obj := valid()
			tt.mutate(obj)
			_, createErr := v.ValidateCreate(context.Background(), obj)
			_, updateErr := v.ValidateUpdate(context.Background(), valid(), obj)
			if (createErr != nil) != tt.wantErr || (updateErr != nil) != tt.wantErr {
				t.Fatalf("create=%v update=%v wantErr=%v", createErr, updateErr, tt.wantErr)
			}
		})
	}

	if _, err := (&ClusterSecurityBaselineCustomValidator{}).ValidateDelete(context.Background(), valid()); err != nil {
		t.Fatalf("delete must be allowed: %v", err)
	}
}
