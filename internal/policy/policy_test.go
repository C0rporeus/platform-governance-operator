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
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var deploymentRef = corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "api"}

func selectorFor(k, v string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{k: v}}
}

func TestSpecificityOf(t *testing.T) {
	tests := []struct {
		name string
		c    Candidate
		want Specificity
	}{
		{"cluster", Candidate{ClusterScoped: true}, SpecificityCluster},
		{"namespace", Candidate{}, SpecificityNamespace},
		{"selector", Candidate{Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("a", "b")}}, SpecificitySelector},
		{"targetRef", Candidate{Targeting: corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef}}, SpecificityTargetRef},
		{"targetRef beats selector", Candidate{Targeting: corev1alpha1.PolicyTargeting{
			TargetRef: &deploymentRef, Selector: selectorFor("a", "b"),
		}}, SpecificityTargetRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SpecificityOf(tt.c); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	subject := Subject{
		Refs:   []corev1alpha1.PolicyTargetReference{{Kind: "Pod", Name: "api-1"}, deploymentRef},
		Labels: map[string]string{"app": "api"},
	}
	tests := []struct {
		name    string
		c       Candidate
		want    bool
		wantErr bool
	}{
		{"no targeting matches", Candidate{}, true, false},
		{"selector match", Candidate{Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "api")}}, true, false},
		{"selector miss", Candidate{Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "web")}}, false, false},
		{"targetRef via owner", Candidate{Targeting: corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef}}, true, false},
		{"targetRef miss", Candidate{Targeting: corev1alpha1.PolicyTargeting{
			TargetRef: &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "other"},
		}}, false, false},
		{"targetRef ignores matching selector", Candidate{Targeting: corev1alpha1.PolicyTargeting{
			TargetRef: &corev1alpha1.PolicyTargetReference{Group: "apps", Kind: "Deployment", Name: "other"},
			Selector:  selectorFor("app", "api"),
		}}, false, false},
		{"invalid selector", Candidate{Targeting: corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Bogus"}},
		}}}, false, true},
		{"cluster policy ignores targetRef", Candidate{ClusterScoped: true, Targeting: corev1alpha1.PolicyTargeting{
			TargetRef: &corev1alpha1.PolicyTargetReference{Kind: "Deployment", Name: "other"},
		}}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Matches(tt.c, subject)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveOrdersBySpecificityThenPriorityThenName(t *testing.T) {
	subject := Subject{Refs: []corev1alpha1.PolicyTargetReference{deploymentRef}, Labels: map[string]string{"app": "api"}}
	candidates := []Candidate{
		{Name: "cluster", ClusterScoped: true, Priority: 100},
		{Name: "ns-low", Namespace: "n", Priority: 1},
		{Name: "ns-high", Namespace: "n", Priority: 5},
		{Name: "sel-b", Namespace: "n", Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "api")}},
		{Name: "sel-a", Namespace: "n", Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "api")}},
		{Name: "ref", Namespace: "n", Targeting: corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef}},
		{Name: "unrelated", Namespace: "n", Targeting: corev1alpha1.PolicyTargeting{Selector: selectorFor("app", "web")}},
	}
	got, errs := Resolve(candidates, subject)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := []string{"ref", "sel-a", "sel-b", "ns-high", "ns-low", "cluster"}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("position %d: got %q, want %q", i, got[i].Name, name)
		}
	}
}

func TestResolveSkipsInvalidSelectorAndReportsError(t *testing.T) {
	bad := Candidate{Name: "bad", Targeting: corev1alpha1.PolicyTargeting{Selector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "Bogus"}},
	}}}
	got, errs := Resolve([]Candidate{bad, {Name: "ok"}}, Subject{})
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("got %+v, want only 'ok'", got)
	}
}

func TestScopeKeys(t *testing.T) {
	tests := []struct {
		name string
		in   corev1alpha1.PolicyTargeting
		want string
	}{
		{"none", corev1alpha1.PolicyTargeting{}, ScopeKeyNamespace},
		{"selector", corev1alpha1.PolicyTargeting{Selector: selectorFor("a", "b")}, ScopeKeySelector},
		{"targetRef", corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef}, "ref:apps/Deployment/api"},
		{"targetRef wins", corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef, Selector: selectorFor("a", "b")}, "ref:apps/Deployment/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScopeKeys(tt.in)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("got %v, want [%s]", got, tt.want)
			}
		})
	}
}

type fakeIndexer struct {
	fields map[string]client.IndexerFunc
	failOn string
}

func (f *fakeIndexer) IndexField(_ context.Context, obj client.Object, field string, fn client.IndexerFunc) error {
	key := obj.GetObjectKind().GroupVersionKind().Kind
	if key == "" {
		key = typeName(obj)
	}
	if key == f.failOn {
		return errors.New("boom")
	}
	f.fields[key+"/"+field] = fn
	return nil
}

func typeName(obj client.Object) string {
	switch obj.(type) {
	case *corev1alpha1.SecurityBaseline:
		return "SecurityBaseline"
	case *corev1alpha1.WorkloadPolicy:
		return "WorkloadPolicy"
	case *corev1alpha1.TelemetryProfile:
		return "TelemetryProfile"
	}
	return "unknown"
}

func TestSetupIndexesRegistersAllKinds(t *testing.T) {
	idx := &fakeIndexer{fields: map[string]client.IndexerFunc{}}
	if err := SetupIndexes(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
	wp := &corev1alpha1.WorkloadPolicy{Spec: corev1alpha1.WorkloadPolicySpec{
		PolicyTargeting: corev1alpha1.PolicyTargeting{TargetRef: &deploymentRef},
	}}
	cases := map[string]client.Object{
		"SecurityBaseline": &corev1alpha1.SecurityBaseline{},
		"WorkloadPolicy":   wp,
		"TelemetryProfile": &corev1alpha1.TelemetryProfile{},
	}
	for kind, obj := range cases {
		fn, ok := idx.fields[kind+"/"+IndexScope]
		if !ok {
			t.Fatalf("%s: index not registered", kind)
		}
		want := ScopeKeyNamespace
		if kind == "WorkloadPolicy" {
			want = "ref:apps/Deployment/api"
		}
		if got := fn(obj); len(got) != 1 || got[0] != want {
			t.Fatalf("%s: got %v, want [%s]", kind, got, want)
		}
	}
}

func TestSetupIndexesPropagatesError(t *testing.T) {
	idx := &fakeIndexer{fields: map[string]client.IndexerFunc{}, failOn: "WorkloadPolicy"}
	if err := SetupIndexes(context.Background(), idx); err == nil {
		t.Fatal("expected error")
	}
}
