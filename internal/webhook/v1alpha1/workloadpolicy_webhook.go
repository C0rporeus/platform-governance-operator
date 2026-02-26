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

	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var workloadpolicylog = logf.Log.WithName("workloadpolicy-resource")

// SetupWorkloadPolicyWebhookWithManager registers the webhook for WorkloadPolicy in the manager.
func SetupWorkloadPolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1alpha1.WorkloadPolicy{}).
		WithValidator(&WorkloadPolicyCustomValidator{}).
		WithDefaulter(&WorkloadPolicyCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-core-platform-f3nr1r-io-v1alpha1-workloadpolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=workloadpolicies,verbs=create;update,versions=v1alpha1,name=mworkloadpolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// WorkloadPolicyCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind WorkloadPolicy when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type WorkloadPolicyCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind WorkloadPolicy.
func (d *WorkloadPolicyCustomDefaulter) Default(_ context.Context, obj *corev1alpha1.WorkloadPolicy) error {
	workloadpolicylog.Info("Defaulting for WorkloadPolicy", "name", obj.GetName())
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-core-platform-f3nr1r-io-v1alpha1-workloadpolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=workloadpolicies,verbs=create;update,versions=v1alpha1,name=vworkloadpolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// WorkloadPolicyCustomValidator struct is responsible for validating the WorkloadPolicy resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type WorkloadPolicyCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type WorkloadPolicy.
func (v *WorkloadPolicyCustomValidator) ValidateCreate(_ context.Context, obj *corev1alpha1.WorkloadPolicy) (admission.Warnings, error) {
	workloadpolicylog.Info("Validation for WorkloadPolicy upon creation", "name", obj.GetName())
	return nil, validateWorkloadPolicySpec(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type WorkloadPolicy.
func (v *WorkloadPolicyCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *corev1alpha1.WorkloadPolicy) (admission.Warnings, error) {
	workloadpolicylog.Info("Validation for WorkloadPolicy upon update", "name", newObj.GetName())
	return nil, validateWorkloadPolicySpec(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type WorkloadPolicy.
func (v *WorkloadPolicyCustomValidator) ValidateDelete(_ context.Context, obj *corev1alpha1.WorkloadPolicy) (admission.Warnings, error) {
	workloadpolicylog.Info("Validation for WorkloadPolicy upon deletion", "name", obj.GetName())
	return nil, nil
}

func validateWorkloadPolicySpec(obj *corev1alpha1.WorkloadPolicy) error {
	for resourceName, resourceValue := range obj.Spec.DefaultRequests {
		if _, err := resource.ParseQuantity(resourceValue); err != nil {
			return fmt.Errorf("invalid defaultRequests quantity for %q: %q", resourceName, resourceValue)
		}
	}

	for resourceName, resourceValue := range obj.Spec.DefaultLimits {
		if _, err := resource.ParseQuantity(resourceValue); err != nil {
			return fmt.Errorf("invalid defaultLimits quantity for %q: %q", resourceName, resourceValue)
		}
	}

	for key, value := range obj.Spec.MandatoryLabels {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("mandatoryLabels key cannot be empty")
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("mandatoryLabels value for key %q cannot be empty", key)
		}
	}

	return nil
}
