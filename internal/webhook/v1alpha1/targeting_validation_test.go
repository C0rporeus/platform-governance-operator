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

func TestValidatorsRejectInvalidTargeting(t *testing.T) {
	t.Parallel()

	badSelector := corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Bogus"}},
	}}
	emptyRef := corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Deployment"}}
	goodRef := corev1alpha1.PolicyTargeting{TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Deployment", Name: "api"}}
	ctx := context.Background()

	for _, bad := range []corev1alpha1.PolicyTargeting{badSelector, emptyRef} {
		if _, err := (&SecurityBaselineCustomValidator{}).ValidateCreate(ctx,
			&corev1alpha1.SecurityBaseline{Spec: corev1alpha1.SecurityBaselineSpec{PolicyTargeting: bad}}); err == nil {
			t.Fatalf("SecurityBaseline: expected rejection of %+v", bad)
		}
		if _, err := (&WorkloadPolicyCustomValidator{}).ValidateCreate(ctx,
			&corev1alpha1.WorkloadPolicy{Spec: corev1alpha1.WorkloadPolicySpec{PolicyTargeting: bad}}); err == nil {
			t.Fatalf("WorkloadPolicy: expected rejection of %+v", bad)
		}
		if _, err := (&TelemetryProfileCustomValidator{}).ValidateUpdate(ctx, nil,
			&corev1alpha1.TelemetryProfile{Spec: corev1alpha1.TelemetryProfileSpec{PolicyTargeting: bad}}); err == nil {
			t.Fatalf("TelemetryProfile: expected rejection of %+v", bad)
		}
	}

	if _, err := (&SecurityBaselineCustomValidator{}).ValidateCreate(ctx,
		&corev1alpha1.SecurityBaseline{Spec: corev1alpha1.SecurityBaselineSpec{PolicyTargeting: goodRef}}); err != nil {
		t.Fatalf("valid targeting rejected: %v", err)
	}
}
