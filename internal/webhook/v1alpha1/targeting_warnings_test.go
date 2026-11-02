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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// A policy whose targeting the early validation cannot evaluate is accepted, but its
// author is told, on create and on update, by every policy kind.
func TestValidatorsWarnAboutTargetingTheEarlyCheckCannotEvaluate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	generated := corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Pod", Name: "web-0"}}
	controllerLabel := corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{
		MatchLabels: map[string]string{"pod-template-hash": "abc"},
	}}
	plain := corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}}

	type call func(corev1alpha1.PolicyTargeting) ([]string, error)
	validators := map[string]call{
		"SecurityBaseline create": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&SecurityBaselineCustomValidator{}).ValidateCreate(ctx,
				&corev1alpha1.SecurityBaseline{Spec: corev1alpha1.SecurityBaselineSpec{PolicyTargeting: p}})
		},
		"SecurityBaseline update": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&SecurityBaselineCustomValidator{}).ValidateUpdate(ctx, nil,
				&corev1alpha1.SecurityBaseline{Spec: corev1alpha1.SecurityBaselineSpec{PolicyTargeting: p}})
		},
		"ClusterSecurityBaseline create": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			p.TargetRef = nil // not supported on a cluster baseline, rejected by the CRD
			return (&ClusterSecurityBaselineCustomValidator{}).ValidateCreate(ctx,
				&corev1alpha1.ClusterSecurityBaseline{Spec: corev1alpha1.ClusterSecurityBaselineSpec{
					SecurityBaselineSpec: corev1alpha1.SecurityBaselineSpec{PolicyTargeting: p}}})
		},
		"WorkloadPolicy create": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&WorkloadPolicyCustomValidator{}).ValidateCreate(ctx,
				&corev1alpha1.WorkloadPolicy{Spec: corev1alpha1.WorkloadPolicySpec{PolicyTargeting: p}})
		},
		"WorkloadPolicy update": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&WorkloadPolicyCustomValidator{}).ValidateUpdate(ctx, nil,
				&corev1alpha1.WorkloadPolicy{Spec: corev1alpha1.WorkloadPolicySpec{PolicyTargeting: p}})
		},
		"TelemetryProfile create": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&TelemetryProfileCustomValidator{}).ValidateCreate(ctx,
				&corev1alpha1.TelemetryProfile{Spec: corev1alpha1.TelemetryProfileSpec{PolicyTargeting: p}})
		},
		"TelemetryProfile update": func(p corev1alpha1.PolicyTargeting) ([]string, error) {
			return (&TelemetryProfileCustomValidator{}).ValidateUpdate(ctx, nil,
				&corev1alpha1.TelemetryProfile{Spec: corev1alpha1.TelemetryProfileSpec{PolicyTargeting: p}})
		},
	}

	for name, validate := range validators {
		if !strings.HasPrefix(name, "ClusterSecurityBaseline") {
			warnings, err := validate(generated)
			if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "Pod names") {
				t.Errorf("%s: a targetRef to a Pod must be accepted with a warning, got %v, %v", name, warnings, err)
			}
		}
		warnings, err := validate(controllerLabel)
		if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "pod-template-hash") {
			t.Errorf("%s: a selector on a controller label must be accepted with a warning, got %v, %v", name, warnings, err)
		}
		if warnings, err := validate(plain); err != nil || len(warnings) != 0 {
			t.Errorf("%s: ordinary targeting must produce no warning, got %v, %v", name, warnings, err)
		}
	}
}
