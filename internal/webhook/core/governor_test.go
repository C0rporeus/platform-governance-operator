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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

func newGovernor(t *testing.T, objs ...*platformv1alpha1.SecurityBaseline) *Governor {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(newWebhookTestScheme(t))
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	return &Governor{Client: b.Build()}
}

// The subject, not the Pod, decides targetRef matching: early workload
// validation evaluates a template Pod that has no name or owners.
func TestGovernorMatchesTargetRefAgainstGivenSubject(t *testing.T) {
	targeted := rootBaseline()
	targeted.Spec.TargetRef = &platformv1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "web"}
	g := newGovernor(t, targeted)
	pod := violatingPod()
	pod.Name = ""

	web := policy.Subject{Refs: []platformv1alpha1.PolicyTargetReference{{Group: "apps", Kind: "Deployment", Name: "web"}}}
	findings, err := g.Evaluate(context.Background(), testNamespace, pod, web)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Violation.Rule != "RunAsNonRoot" {
		t.Fatalf("expected one RunAsNonRoot finding for the targeted Deployment, got %+v", findings)
	}

	other := policy.Subject{Refs: []platformv1alpha1.PolicyTargetReference{{Group: "apps", Kind: "Deployment", Name: "api"}}}
	findings, err = g.Evaluate(context.Background(), testNamespace, pod, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings for a Deployment the policy does not target, got %+v", findings)
	}
}

func TestGovernorNormalisesEmptyActionToEnforce(t *testing.T) {
	g := newGovernor(t, rootBaseline())
	findings, err := g.Evaluate(context.Background(), testNamespace, violatingPod(), policy.SubjectForPod(violatingPod()))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	for _, f := range findings {
		if f.Action != platformv1alpha1.EnforcementActionEnforce {
			t.Errorf("action = %q, want Enforce", f.Action)
		}
		if f.Source == nil || f.Source.GetName() != "strict" {
			t.Errorf("source = %v, want the strict baseline", f.Source)
		}
	}
}

func TestFindingMessageNamesRuleControlAndPolicy(t *testing.T) {
	f := Finding{
		Violation: SecurityViolation{Rule: "RunAsNonRoot", ControlISO: isoControlA82, Message: "must run as non-root"},
		Policy:    "cluster/corp",
		Source:    &platformv1alpha1.ClusterSecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "corp"}},
	}
	msg := f.Message()
	for _, want := range []string{"RunAsNonRoot", isoControlA82, "must run as non-root", "policy: cluster/corp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}
