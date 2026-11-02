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
func TestPodWebhooksExcludeSystemAndOperatorNamespaces(t *testing.T) {
	kustomization := readAll(t, "config/default/kustomization.yaml")[0]
	operatorNamespace, _ := kustomization["namespace"].(string)
	if operatorNamespace == "" {
		t.Fatal("config/default/kustomization.yaml has no namespace")
	}

	want := []string{"kube-system", "kube-public", "kube-node-lease", "cert-manager", operatorNamespace}
	patched := webhooksOf(readAll(t, "config/webhook/patches/namespace-selector.yaml"))
	for _, name := range []string{"mpod.kb.io", "vpod.kb.io"} {
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
