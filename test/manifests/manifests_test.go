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

// Package manifests guards availability invariants of the deployment manifests.
// The Pod webhooks use failurePolicy: Fail (ADR-0002), so a cold install, a node
// drain or a typo in an exclusion can stop Pod creation cluster-wide.
package manifests

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

type object = map[string]any

func readAll(t *testing.T, rel string) []object {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	var out []object
	for {
		var o object
		if err := dec.Decode(&o); err != nil {
			if errors.Is(err, io.EOF) {
				return out
			}
			t.Fatalf("%s: %v", rel, err)
		}
		if len(o) > 0 {
			out = append(out, o)
		}
	}
}

func get(o any, path ...string) any {
	for _, p := range path {
		m, ok := o.(object)
		if !ok {
			return nil
		}
		o = m[p]
	}
	return o
}

func webhooksOf(docs []object) map[string]object {
	out := map[string]object{}
	for _, d := range docs {
		items, _ := d["webhooks"].([]any)
		for _, w := range items {
			wh := w.(object)
			out[wh["name"].(string)] = wh
		}
	}
	return out
}

func TestEveryWebhookHasAnExplicitShortTimeout(t *testing.T) {
	for name, wh := range webhooksOf(readAll(t, "config/webhook/manifests.yaml")) {
		timeout, ok := wh["timeoutSeconds"].(float64)
		if !ok || timeout <= 0 || timeout > 10 {
			t.Errorf("%s: timeoutSeconds must be set and at most 10, got %v", name, wh["timeoutSeconds"])
		}
	}
}

// The webhook patch must exclude the operator's own namespace, matched on the
// immutable kubernetes.io/metadata.name label, and that name must equal the
// namespace Kustomize deploys into. A mismatch silently excludes nothing.
func TestPodAndWorkloadWebhooksExcludeSystemAndOperatorNamespaces(t *testing.T) {
	kustomization := readAll(t, "config/default/kustomization.yaml")[0]
	operatorNamespace, _ := kustomization["namespace"].(string)
	if operatorNamespace == "" {
		t.Fatal("config/default/kustomization.yaml has no namespace")
	}

	want := []string{"kube-system", "kube-public", "kube-node-lease", "cert-manager", operatorNamespace}
	patched := webhooksOf(readAll(t, "config/webhook/patches/namespace-selector.yaml"))
	for _, name := range []string{"mpod.kb.io", "vpod.kb.io", "vworkload.kb.io"} {
		wh, ok := patched[name]
		if !ok {
			t.Fatalf("%s is not patched", name)
		}
		exprs, _ := get(wh, "namespaceSelector", "matchExpressions").([]any)
		if len(exprs) != 1 {
			t.Fatalf("%s: expected one matchExpression, got %d", name, len(exprs))
		}
		expr := exprs[0].(object)
		if expr["key"] != "kubernetes.io/metadata.name" || expr["operator"] != "NotIn" {
			t.Fatalf("%s: exclusion must use NotIn on kubernetes.io/metadata.name, got %v %v",
				name, expr["key"], expr["operator"])
		}
		var values []string
		for _, v := range expr["values"].([]any) {
			values = append(values, v.(string))
		}
		for _, ns := range want {
			if !slices.Contains(values, ns) {
				t.Errorf("%s does not exclude namespace %q", name, ns)
			}
		}
	}
}

func TestOperatorRunsHighlyAvailable(t *testing.T) {
	var deployment, pdb object
	for _, d := range readAll(t, "config/manager/manager.yaml") {
		if d["kind"] == "Deployment" {
			deployment = d
		}
	}
	for _, d := range readAll(t, "config/manager/pdb.yaml") {
		if d["kind"] == "PodDisruptionBudget" {
			pdb = d
		}
	}
	if deployment == nil || pdb == nil {
		t.Fatal("manager Deployment and PodDisruptionBudget must both exist")
	}
	if replicas, _ := get(deployment, "spec", "replicas").(float64); replicas < 2 {
		t.Errorf("replicas = %v, want at least 2", replicas)
	}
	if get(pdb, "spec", "minAvailable") != float64(1) {
		t.Errorf("PodDisruptionBudget minAvailable = %v, want 1", get(pdb, "spec", "minAvailable"))
	}
	if get(deployment, "spec", "template", "spec", "affinity", "podAntiAffinity") == nil {
		t.Error("anti-affinity is required so replicas are not co-located")
	}
	kustom := readAll(t, "config/manager/kustomization.yaml")[0]
	resources, _ := kustom["resources"].([]any)
	if !slices.Contains(resources, any("pdb.yaml")) {
		t.Error("pdb.yaml must be listed in config/manager/kustomization.yaml")
	}
}

// Kubernetes Security Checklist: a memory limit above the request can expose the node to OOM.
func TestManagerMemoryLimitDoesNotExceedRequest(t *testing.T) {
	for _, d := range readAll(t, "config/manager/manager.yaml") {
		if d["kind"] != "Deployment" {
			continue
		}
		containers, _ := get(d, "spec", "template", "spec", "containers").([]any)
		for _, c := range containers {
			request := get(c, "resources", "requests", "memory")
			limit := get(c, "resources", "limits", "memory")
			if request != limit {
				t.Errorf("memory request %v must equal limit %v", request, limit)
			}
		}
	}
}

// ADR-0002: the Pod webhooks are the security boundary and fail closed.
// ADR-0003: the early workload validation is feedback and fails open; if it failed
// closed, an operator outage would also block the repair of the operator itself.
func TestFailurePolicyFollowsTheADRs(t *testing.T) {
	webhooks := webhooksOf(readAll(t, "config/webhook/manifests.yaml"))
	want := map[string]string{"mpod.kb.io": "Fail", "vpod.kb.io": "Fail", "vworkload.kb.io": "Ignore"}
	for name, policy := range want {
		wh, ok := webhooks[name]
		if !ok {
			t.Fatalf("%s is not in config/webhook/manifests.yaml", name)
		}
		if wh["failurePolicy"] != policy {
			t.Errorf("%s: failurePolicy = %v, want %s", name, wh["failurePolicy"], policy)
		}
	}
}

// The early validation only has a Pod template to evaluate on create or when the
// template changes. The API server filters the rest with a match condition, so the
// controllers' constant updates to workloads never reach the webhook.
func TestEarlyValidationIsOnlyCalledWhenThePodTemplateChanges(t *testing.T) {
	wh, ok := webhooksOf(readAll(t, "config/webhook/patches/namespace-selector.yaml"))["vworkload.kb.io"]
	if !ok {
		t.Fatal("vworkload.kb.io is not patched")
	}
	conditions, _ := wh["matchConditions"].([]any)
	if len(conditions) != 1 {
		t.Fatalf("expected one match condition, got %d", len(conditions))
	}
	expression, _ := conditions[0].(object)["expression"].(string)
	for _, want := range []string{
		"request.operation == 'CREATE'",
		"object.spec.template != oldObject.spec.template",
		"request.kind.kind == 'CronJob'",
		"object.spec.jobTemplate.spec.template != oldObject.spec.jobTemplate.spec.template",
	} {
		if !strings.Contains(expression, want) {
			t.Errorf("match condition %q does not contain %q", expression, want)
		}
	}
}

// The dry-run overlay (ADR-0004) gives the operator `create pods`. RBAC cannot limit a
// verb to dry runs, so the safeguard policy is the only thing between a compromised
// operator and real Pods. Its identity is a literal and must match what Kustomize deploys.
func TestDryRunSafeguardNamesTheDeployedIdentity(t *testing.T) {
	// Identity built from config/default: <namespace>:<namePrefix><ServiceAccount>.
	def := string(mustRead(t, "config/default/kustomization.yaml"))
	namespace := kustomizeScalar(t, def, "namespace")
	prefix := kustomizeScalar(t, def, "namePrefix")
	sa := get(readAll(t, "config/rbac/service_account.yaml")[0], "metadata", "name").(string)
	username := "system:serviceaccount:" + namespace + ":" + prefix + sa

	safeguard := readAll(t, "config/dry-run/dryrun-safeguard.yaml")
	var policy, binding object
	for _, d := range safeguard {
		switch d["kind"] {
		case "ValidatingAdmissionPolicy":
			policy = d
		case "ValidatingAdmissionPolicyBinding":
			binding = d
		}
	}
	if policy == nil || binding == nil {
		t.Fatal("the safeguard needs a ValidatingAdmissionPolicy and its binding")
	}
	conditions, _ := get(policy, "spec", "matchConditions").([]any)
	if len(conditions) != 1 || !strings.Contains(get(conditions[0], "expression").(string), "'"+username+"'") {
		t.Errorf("the policy must match the operator identity %q", username)
	}
	validations, _ := get(policy, "spec", "validations").([]any)
	if len(validations) != 1 || !strings.Contains(get(validations[0], "expression").(string), "request.dryRun == true") {
		t.Error("the policy must require request.dryRun == true")
	}
	if get(policy, "spec", "failurePolicy") != "Fail" {
		t.Error("the safeguard must fail closed")
	}
	if get(binding, "spec", "policyName") != get(policy, "metadata", "name") {
		t.Error("the binding must reference the policy")
	}
	if actions, _ := get(binding, "spec", "validationActions").([]any); len(actions) != 1 || actions[0] != "Deny" {
		t.Error("the binding must Deny, not only Warn or Audit")
	}

	for _, d := range readAll(t, "config/dry-run/dryrun-rbac.yaml") {
		if d["kind"] != "ClusterRoleBinding" {
			continue
		}
		subject := get(d, "subjects").([]any)[0]
		if get(subject, "name") != prefix+sa || get(subject, "namespace") != namespace {
			t.Errorf("the dry-run binding must target %s/%s", namespace, prefix+sa)
		}
	}
}

// The default install must not be able to create Pods: the permission is opt-in.
func TestDefaultInstallCannotCreatePods(t *testing.T) {
	for _, d := range readAll(t, "config/rbac/role.yaml") {
		rules, _ := d["rules"].([]any)
		for _, r := range rules {
			resources, _ := get(r, "resources").([]any)
			verbs, _ := get(r, "verbs").([]any)
			if slices.Contains(resources, any("pods")) && slices.Contains(verbs, any("create")) {
				t.Fatal("config/rbac must not grant create on pods; it belongs to the opt-in config/dry-run overlay")
			}
		}
	}
}

func mustRead(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func kustomizeScalar(t *testing.T, doc, key string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if value, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(value)
		}
	}
	t.Fatalf("%s not found in config/default/kustomization.yaml", key)
	return ""
}
