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
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

const operatorNS = "pgo-system"

func requestIn(t *testing.T, namespace string, pod *corev1.Pod) admission.Request {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Namespace: namespace,
		Object:    runtime.RawExtension{Raw: raw},
	}}
}

func clusterBaseline(name string, mutate func(*platformv1alpha1.ClusterSecurityBaselineSpec)) *platformv1alpha1.ClusterSecurityBaseline {
	cb := &platformv1alpha1.ClusterSecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.ClusterSecurityBaselineSpec{
			SecurityBaselineSpec: platformv1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
		},
	}
	if mutate != nil {
		mutate(&cb.Spec)
	}
	return cb
}

func namespaceObj(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func runClusterValidator(t *testing.T, namespace string, pod *corev1.Pod, objs ...client.Object) admission.Response {
	t.Helper()
	scheme := newWebhookTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	validator := &PodValidator{
		Client:            cl,
		Recorder:          record.NewFakeRecorder(20),
		OperatorNamespace: operatorNS,
		decoder:           admission.NewDecoder(scheme),
	}
	return validator.Handle(context.Background(), requestIn(t, namespace, pod))
}

func TestClusterBaselineDeniesInAnyNamespace(t *testing.T) {
	t.Parallel()

	resp := runClusterValidator(t, "team-a", &corev1.Pod{}, clusterBaseline("corp", nil))

	if resp.Allowed {
		t.Fatalf("expected denial from cluster baseline")
	}
	if !strings.Contains(resp.Result.Message, "cluster/corp") {
		t.Fatalf("denial should name the cluster policy, got %q", resp.Result.Message)
	}
}

func TestNamespacedBaselineCannotWeakenClusterBaseline(t *testing.T) {
	t.Parallel()

	local := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "lenient", Namespace: "team-a"},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			EnforcementAction: platformv1alpha1.EnforcementActionAudit,
		},
	}
	resp := runClusterValidator(t, "team-a", &corev1.Pod{}, clusterBaseline("corp", nil), local)

	if resp.Allowed {
		t.Fatalf("a lenient namespaced baseline must not override the cluster baseline")
	}
}

func TestClusterBaselineHonorsActionMode(t *testing.T) {
	t.Parallel()

	warn := clusterBaseline("warn-only", func(s *platformv1alpha1.ClusterSecurityBaselineSpec) {
		s.EnforcementAction = platformv1alpha1.EnforcementActionWarn
	})
	resp := runClusterValidator(t, "team-a", &corev1.Pod{}, warn)

	if !resp.Allowed || len(resp.Warnings) == 0 {
		t.Fatalf("expected allowed with warnings, got allowed=%v warnings=%v", resp.Allowed, resp.Warnings)
	}
}

func TestClusterBaselineSkipsExcludedNamespaces(t *testing.T) {
	t.Parallel()

	explicit := clusterBaseline("corp", func(s *platformv1alpha1.ClusterSecurityBaselineSpec) {
		s.ExcludedNamespaces = []string{"legacy"}
	})
	for _, ns := range []string{"kube-system", "kube-public", "cert-manager", operatorNS, "legacy"} {
		t.Run(ns, func(t *testing.T) {
			t.Parallel()
			if resp := runClusterValidator(t, ns, &corev1.Pod{}, explicit); !resp.Allowed {
				t.Fatalf("namespace %q must be excluded from cluster baseline: %v", ns, resp.Result)
			}
		})
	}
}

func TestClusterBaselineNamespaceSelector(t *testing.T) {
	t.Parallel()

	prodOnly := clusterBaseline("prod-only", func(s *platformv1alpha1.ClusterSecurityBaselineSpec) {
		s.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}
	})

	t.Run("matching namespace is governed", func(t *testing.T) {
		t.Parallel()
		resp := runClusterValidator(t, "team-a", &corev1.Pod{}, prodOnly, namespaceObj("team-a", map[string]string{"env": "prod"}))
		if resp.Allowed {
			t.Fatalf("expected denial in a prod namespace")
		}
	})
	t.Run("non-matching namespace is not governed", func(t *testing.T) {
		t.Parallel()
		resp := runClusterValidator(t, "team-a", &corev1.Pod{}, prodOnly, namespaceObj("team-a", map[string]string{"env": "dev"}))
		if !resp.Allowed {
			t.Fatalf("expected allow in a dev namespace: %v", resp.Result)
		}
	})
	t.Run("namespace lookup failure fails closed", func(t *testing.T) {
		t.Parallel()
		resp := runClusterValidator(t, "team-a", &corev1.Pod{}, prodOnly)
		if resp.Allowed {
			t.Fatalf("a failed namespace lookup must not silently allow the Pod")
		}
		if resp.Result == nil || resp.Result.Code != 500 {
			t.Fatalf("expected a 500 error response, got %+v", resp.Result)
		}
	})
}

func TestClusterBaselinePodSelector(t *testing.T) {
	t.Parallel()

	dbOnly := clusterBaseline("db-only", func(s *platformv1alpha1.ClusterSecurityBaselineSpec) {
		s.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "db"}}
	})

	web := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"tier": "web"}}}
	if resp := runClusterValidator(t, "team-a", web, dbOnly); !resp.Allowed {
		t.Fatalf("pod outside the selector must be allowed: %v", resp.Result)
	}
	db := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"tier": "db"}}}
	if resp := runClusterValidator(t, "team-a", db, dbOnly); resp.Allowed {
		t.Fatalf("pod inside the selector must be denied")
	}
}

func TestClusterBaselineInvalidNamespaceSelectorIsSkipped(t *testing.T) {
	t.Parallel()

	bad := clusterBaseline("bad", func(s *platformv1alpha1.ClusterSecurityBaselineSpec) {
		s.NamespaceSelector = &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Bogus"}},
		}
	})
	resp := runClusterValidator(t, "team-a", &corev1.Pod{}, bad, namespaceObj("team-a", nil))
	if !resp.Allowed {
		t.Fatalf("invalid selector is skipped (rejected earlier by the CRD webhook): %v", resp.Result)
	}
}
