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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// IndexScope is the field index registered on every namespaced policy kind.
// Listing with client.MatchingFields{IndexScope: key} reads from the informer
// cache instead of the API server.
const IndexScope = "spec.policyScope"

const (
	// ScopeKeySelector is the index key of policies that use a label selector.
	ScopeKeySelector = "selector"
	// ScopeKeyNamespace is the index key of policies without targeting.
	ScopeKeyNamespace = "namespace"
)

// TargetRefKey is the index key of a policy attached to ref.
func TargetRefKey(ref corev1alpha1.PolicyTargetReference) string {
	return fmt.Sprintf("ref:%s/%s/%s", ref.Group, ref.Kind, ref.Name)
}

// ScopeKeys returns the index keys for a policy's targeting. A targetRef wins
// over a selector, mirroring SpecificityOf.
func ScopeKeys(t corev1alpha1.PolicyTargeting) []string {
	switch {
	case t.TargetRef != nil:
		return []string{TargetRefKey(*t.TargetRef)}
	case t.Selector != nil:
		return []string{ScopeKeySelector}
	default:
		return []string{ScopeKeyNamespace}
	}
}

// SetupIndexes registers IndexScope for SecurityBaseline, WorkloadPolicy and
// TelemetryProfile. It must run before the manager starts.
func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	registrations := []struct {
		obj client.Object
		fn  client.IndexerFunc
	}{
		{&corev1alpha1.SecurityBaseline{}, func(o client.Object) []string {
			return ScopeKeys(o.(*corev1alpha1.SecurityBaseline).Spec.PolicyTargeting)
		}},
		{&corev1alpha1.WorkloadPolicy{}, func(o client.Object) []string {
			return ScopeKeys(o.(*corev1alpha1.WorkloadPolicy).Spec.PolicyTargeting)
		}},
		{&corev1alpha1.TelemetryProfile{}, func(o client.Object) []string {
			return ScopeKeys(o.(*corev1alpha1.TelemetryProfile).Spec.PolicyTargeting)
		}},
	}
	for _, r := range registrations {
		if err := indexer.IndexField(ctx, r.obj, IndexScope, r.fn); err != nil {
			return fmt.Errorf("index %T: %w", r.obj, err)
		}
	}
	return nil
}
