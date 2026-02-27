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
	"net/url"
	"strconv"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var telemetryprofilelog = logf.Log.WithName("telemetryprofile-resource")

// SetupTelemetryProfileWebhookWithManager registers the webhook for TelemetryProfile in the manager.
func SetupTelemetryProfileWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1alpha1.TelemetryProfile{}).
		WithValidator(&TelemetryProfileCustomValidator{}).
		WithDefaulter(&TelemetryProfileCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-platform-f3nr1r-io-v1alpha1-telemetryprofile,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=telemetryprofiles,verbs=create;update,versions=v1alpha1,name=mtelemetryprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// TelemetryProfileCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind TelemetryProfile when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TelemetryProfileCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind TelemetryProfile.
func (d *TelemetryProfileCustomDefaulter) Default(_ context.Context, obj *corev1alpha1.TelemetryProfile) error {
	telemetryprofilelog.Info("Defaulting for TelemetryProfile", "name", obj.GetName())
	return nil
}

// +kubebuilder:webhook:path=/validate-core-platform-f3nr1r-io-v1alpha1-telemetryprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.platform.f3nr1r.io,resources=telemetryprofiles,verbs=create;update,versions=v1alpha1,name=vtelemetryprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// TelemetryProfileCustomValidator struct is responsible for validating the TelemetryProfile resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TelemetryProfileCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TelemetryProfile.
func (v *TelemetryProfileCustomValidator) ValidateCreate(_ context.Context, obj *corev1alpha1.TelemetryProfile) (admission.Warnings, error) {
	telemetryprofilelog.Info("Validation for TelemetryProfile upon creation", "name", obj.GetName())
	return nil, validateTelemetryProfileSpec(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TelemetryProfile.
func (v *TelemetryProfileCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *corev1alpha1.TelemetryProfile) (admission.Warnings, error) {
	telemetryprofilelog.Info("Validation for TelemetryProfile upon update", "name", newObj.GetName())
	return nil, validateTelemetryProfileSpec(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TelemetryProfile.
func (v *TelemetryProfileCustomValidator) ValidateDelete(_ context.Context, obj *corev1alpha1.TelemetryProfile) (admission.Warnings, error) {
	telemetryprofilelog.Info("Validation for TelemetryProfile upon deletion", "name", obj.GetName())
	return nil, nil
}

func validateTelemetryProfileSpec(obj *corev1alpha1.TelemetryProfile) error {
	if obj.Spec.TracingEndpoint != "" {
		if _, err := url.ParseRequestURI(obj.Spec.TracingEndpoint); err != nil {
			return fmt.Errorf("invalid tracingEndpoint: %q", obj.Spec.TracingEndpoint)
		}
	}

	if obj.Spec.SamplingRate != "" {
		rate, err := strconv.ParseFloat(obj.Spec.SamplingRate, 64)
		if err != nil {
			return fmt.Errorf("invalid samplingRate: %q", obj.Spec.SamplingRate)
		}
		if rate < 0 || rate > 1 {
			return fmt.Errorf("samplingRate must be between 0 and 1")
		}
	}

	return nil
}
