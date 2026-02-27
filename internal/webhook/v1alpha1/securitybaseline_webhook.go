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
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var securitybaselinelog = logf.Log.WithName("securitybaseline-resource")

// SetupSecurityBaselineWebhookWithManager registers the webhook for SecurityBaseline in the manager.
func SetupSecurityBaselineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1alpha1.SecurityBaseline{}).
		WithValidator(&SecurityBaselineCustomValidator{}).
		WithDefaulter(&SecurityBaselineCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-platform-f3nr1r-io-v1alpha1-securitybaseline,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=securitybaselines,verbs=create;update,versions=v1alpha1,name=msecuritybaseline-v1alpha1.kb.io,admissionReviewVersions=v1

// SecurityBaselineCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind SecurityBaseline when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type SecurityBaselineCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind SecurityBaseline.
func (d *SecurityBaselineCustomDefaulter) Default(_ context.Context, obj *corev1alpha1.SecurityBaseline) error {
	securitybaselinelog.Info("Defaulting for SecurityBaseline", "name", obj.GetName())
	return nil
}

// +kubebuilder:webhook:path=/validate-core-platform-f3nr1r-io-v1alpha1-securitybaseline,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=securitybaselines,verbs=create;update,versions=v1alpha1,name=vsecuritybaseline-v1alpha1.kb.io,admissionReviewVersions=v1

// SecurityBaselineCustomValidator struct is responsible for validating the SecurityBaseline resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type SecurityBaselineCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SecurityBaseline.
func (v *SecurityBaselineCustomValidator) ValidateCreate(_ context.Context, obj *corev1alpha1.SecurityBaseline) (admission.Warnings, error) {
	securitybaselinelog.Info("Validation for SecurityBaseline upon creation", "name", obj.GetName())
	return nil, validateSecurityBaselineSpec(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type SecurityBaseline.
func (v *SecurityBaselineCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *corev1alpha1.SecurityBaseline) (admission.Warnings, error) {
	securitybaselinelog.Info("Validation for SecurityBaseline upon update", "name", newObj.GetName())
	return nil, validateSecurityBaselineSpec(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type SecurityBaseline.
func (v *SecurityBaselineCustomValidator) ValidateDelete(_ context.Context, obj *corev1alpha1.SecurityBaseline) (admission.Warnings, error) {
	securitybaselinelog.Info("Validation for SecurityBaseline upon deletion", "name", obj.GetName())
	return nil, nil
}

func validateSecurityBaselineSpec(obj *corev1alpha1.SecurityBaseline) error {
	for _, namespace := range obj.Spec.ExcludedNamespaces {
		if strings.TrimSpace(namespace) == "" {
			return fmt.Errorf("excludedNamespaces entries cannot be empty")
		}
	}
	return nil
}
