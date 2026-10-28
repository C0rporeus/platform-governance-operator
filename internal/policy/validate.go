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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// ValidateTargeting rejects targeting that could never be evaluated: an invalid
// label selector or a targetRef without kind or name. Rejecting it at write
// time avoids policies that are silently skipped at admission.
func ValidateTargeting(t corev1alpha1.PolicyTargeting) error {
	if ref := t.TargetRef; ref != nil {
		if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.Name) == "" {
			return fmt.Errorf("targetRef.kind and targetRef.name are required")
		}
	}
	if t.Selector != nil {
		if _, err := metav1.LabelSelectorAsSelector(t.Selector); err != nil {
			return fmt.Errorf("invalid selector: %w", err)
		}
	}
	return nil
}
