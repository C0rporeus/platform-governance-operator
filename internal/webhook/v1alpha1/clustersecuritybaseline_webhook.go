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

package v1alpha1

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var clustersecuritybaselinelog = logf.Log.WithName("clustersecuritybaseline-resource")

// SetupClusterSecurityBaselineWebhookWithManager registers the validating webhook for ClusterSecurityBaseline.
func SetupClusterSecurityBaselineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1alpha1.ClusterSecurityBaseline{}).
		WithValidator(&ClusterSecurityBaselineCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-core-platform-f3nr1r-io-v1alpha1-clustersecuritybaseline,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=clustersecuritybaselines,verbs=create;update,versions=v1alpha1,name=vclustersecuritybaseline-v1alpha1.kb.io,admissionReviewVersions=v1

// ClusterSecurityBaselineCustomValidator validates ClusterSecurityBaseline resources.
// An invalid namespaceSelector would silently leave namespaces unprotected, so it is
// rejected here instead of being skipped at admission time.
type ClusterSecurityBaselineCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator.
func (v *ClusterSecurityBaselineCustomValidator) ValidateCreate(_ context.Context, obj *corev1alpha1.ClusterSecurityBaseline) (admission.Warnings, error) {
	clustersecuritybaselinelog.Info("Validation for ClusterSecurityBaseline upon creation", "name", obj.GetName())
	return nil, validateClusterSecurityBaselineSpec(&obj.Spec)
}

// ValidateUpdate implements webhook.CustomValidator.
func (v *ClusterSecurityBaselineCustomValidator) ValidateUpdate(_ context.Context, _, newObj *corev1alpha1.ClusterSecurityBaseline) (admission.Warnings, error) {
	clustersecuritybaselinelog.Info("Validation for ClusterSecurityBaseline upon update", "name", newObj.GetName())
	return nil, validateClusterSecurityBaselineSpec(&newObj.Spec)
}

// ValidateDelete implements webhook.CustomValidator.
func (v *ClusterSecurityBaselineCustomValidator) ValidateDelete(_ context.Context, _ *corev1alpha1.ClusterSecurityBaseline) (admission.Warnings, error) {
	return nil, nil
}

func validateClusterSecurityBaselineSpec(spec *corev1alpha1.ClusterSecurityBaselineSpec) error {
	if spec.TargetRef != nil {
		return fmt.Errorf("targetRef is not supported on a ClusterSecurityBaseline; use selector or namespaceSelector")
	}
	if spec.NamespaceSelector != nil {
		if _, err := metav1.LabelSelectorAsSelector(spec.NamespaceSelector); err != nil {
			return fmt.Errorf("invalid namespaceSelector: %w", err)
		}
	}
	return validateSecurityBaselineSpec(&spec.SecurityBaselineSpec)
}
