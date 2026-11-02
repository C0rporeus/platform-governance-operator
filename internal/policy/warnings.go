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
	"fmt"
	"slices"
	"strings"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// controllerManagedLabels are labels that workload controllers add to the Pods they
// create. They are not in a workload's Pod template, so the early validation of
// workloads cannot see them.
var controllerManagedLabels = []string{
	"apps.kubernetes.io/pod-index",
	"batch.kubernetes.io/controller-uid",
	"batch.kubernetes.io/job-completion-index",
	"batch.kubernetes.io/job-name",
	"controller-revision-hash",
	"controller-uid",
	"job-name",
	"pod-template-generation",
	"pod-template-hash",
	"statefulset.kubernetes.io/pod-name",
}

// TargetingWarnings describes the parts of a policy's targeting that the early
// validation of workloads (kubectl apply) cannot evaluate. The policy is valid and
// still applies when the Pods are created; the warning tells its author that the
// early feedback will not reflect it, instead of leaving that to be discovered.
func TargetingWarnings(t corev1alpha1.PolicyTargeting) []string {
	var warnings []string

	if ref := t.TargetRef; ref != nil {
		generated := (ref.Group == "" && ref.Kind == kindPod) || (ref.Group == appsGroup && ref.Kind == kindReplicaSet)
		if generated {
			warnings = append(warnings, fmt.Sprintf(
				"targetRef to %s %q is only matched when Pods are created: Pod names and ReplicaSet hashes are generated, "+
					"so the early validation of workloads cannot evaluate it. Prefer a targetRef to the Deployment or "+
					"StatefulSet, or a selector on the Pod template labels", ref.Kind, ref.Name))
		}
	}

	if t.Selector != nil {
		used := map[string]bool{}
		for key := range t.Selector.MatchLabels {
			used[key] = true
		}
		for _, expr := range t.Selector.MatchExpressions {
			used[expr.Key] = true
		}
		var managed []string
		for key := range used {
			if slices.Contains(controllerManagedLabels, key) {
				managed = append(managed, key)
			}
		}
		if len(managed) > 0 {
			slices.Sort(managed)
			warnings = append(warnings, fmt.Sprintf(
				"selector uses %s, set by the workload controllers after the Pod exists: the early validation of workloads "+
					"cannot see it and the policy is only matched when Pods are created", strings.Join(managed, ", ")))
		}
	}
	return warnings
}
