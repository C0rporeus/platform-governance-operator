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
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// DefaultExcludedNamespaces never receive cluster-wide baselines, so cluster
// policy cannot block the control plane or the components admission depends on.
var DefaultExcludedNamespaces = []string{"kube-system", "kube-public", "cert-manager"}

// IsClusterExcluded reports whether a cluster-wide baseline must skip the
// namespace: one of the defaults, the operator's own namespace (when known) or
// an entry of the baseline's excludedNamespaces.
func IsClusterExcluded(namespace, operatorNamespace string, excluded []string) bool {
	return slices.Contains(DefaultExcludedNamespaces, namespace) ||
		(operatorNamespace != "" && namespace == operatorNamespace) ||
		slices.Contains(excluded, namespace)
}

// NamespaceMatches reports whether namespace labels satisfy selector. A nil
// selector matches every namespace.
func NamespaceMatches(selector *metav1.LabelSelector, namespaceLabels map[string]string) (bool, error) {
	if selector == nil {
		return true, nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}
	return parsed.Matches(labels.Set(namespaceLabels)), nil
}
