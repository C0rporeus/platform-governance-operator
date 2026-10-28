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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsClusterExcluded(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		operatorNS string
		excluded   []string
		want       bool
	}{
		{"kube-system default", "kube-system", "", nil, true},
		{"kube-public default", "kube-public", "", nil, true},
		{"cert-manager default", "cert-manager", "", nil, true},
		{"operator namespace", "pgo-system", "pgo-system", nil, true},
		{"operator namespace unknown", "pgo-system", "", nil, false},
		{"explicit exclusion", "legacy", "", []string{"legacy"}, true},
		{"regular namespace", "team-a", "pgo-system", []string{"legacy"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsClusterExcluded(tt.namespace, tt.operatorNS, tt.excluded); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamespaceMatches(t *testing.T) {
	prod := &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}
	bad := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Bogus"}}}

	if ok, err := NamespaceMatches(nil, nil); err != nil || !ok {
		t.Fatalf("nil selector must match, got %v %v", ok, err)
	}
	if ok, _ := NamespaceMatches(prod, map[string]string{"env": "prod"}); !ok {
		t.Fatal("expected match")
	}
	if ok, _ := NamespaceMatches(prod, map[string]string{"env": "dev"}); ok {
		t.Fatal("expected miss")
	}
	if _, err := NamespaceMatches(bad, nil); err == nil {
		t.Fatal("expected error for invalid selector")
	}
}
