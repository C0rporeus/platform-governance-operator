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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnforcementAction defines how non-compliant workloads are handled.
// +kubebuilder:validation:Enum=Enforce;Warn;Audit
type EnforcementAction string

const (
	// EnforcementActionEnforce blocks non-compliant admission requests.
	EnforcementActionEnforce EnforcementAction = "Enforce"
	// EnforcementActionWarn allows admission but returns warning messages to the client.
	EnforcementActionWarn EnforcementAction = "Warn"
	// EnforcementActionAudit allows admission silently and records violations in events and status.
	EnforcementActionAudit EnforcementAction = "Audit"
)

// SecurityBaselineSpec defines the desired state of SecurityBaseline
type SecurityBaselineSpec struct {
	// EnforcementAction determines the behavior when a violation occurs.
	// Can be Enforce, Warn, or Audit. Defaults to Enforce.
	// +kubebuilder:validation:Enum=Enforce;Warn;Audit
	// +kubebuilder:default=Enforce
	// +optional
	EnforcementAction EnforcementAction `json:"enforcementAction,omitempty"`

	// Require running as non-root
	// +kubebuilder:default=true
	RunAsNonRoot bool `json:"runAsNonRoot"`

	// Require a read-only root filesystem
	// +kubebuilder:default=true
	ReadOnlyRootFilesystem bool `json:"readOnlyRootFilesystem"`

	// DisallowPrivilegeEscalation requires allowPrivilegeEscalation=false in every container.
	// +optional
	DisallowPrivilegeEscalation *bool `json:"disallowPrivilegeEscalation,omitempty"`

	// DisallowPrivileged rejects containers that run in privileged mode.
	// +optional
	DisallowPrivileged *bool `json:"disallowPrivileged,omitempty"`

	// RequireDropAllCapabilities requires dropping ALL Linux capabilities.
	// +optional
	RequireDropAllCapabilities *bool `json:"requireDropAllCapabilities,omitempty"`

	// AllowedCapabilities lists capabilities permitted in capabilities.add when requiring drop ALL.
	// +optional
	AllowedCapabilities []corev1.Capability `json:"allowedCapabilities,omitempty"`

	// RequireSeccompProfile requires RuntimeDefault or Localhost seccomp at Pod or container level.
	// +optional
	RequireSeccompProfile *bool `json:"requireSeccompProfile,omitempty"`

	// DisallowHostNamespaces rejects hostNetwork, hostPID, and hostIPC.
	// +optional
	DisallowHostNamespaces *bool `json:"disallowHostNamespaces,omitempty"`

	// DisallowHostPathVolumes rejects volumes that mount host paths.
	// +optional
	DisallowHostPathVolumes *bool `json:"disallowHostPathVolumes,omitempty"`

	// DisallowLatestTag rejects latest and untagged image references.
	// +optional
	DisallowLatestTag *bool `json:"disallowLatestTag,omitempty"`

	// RequireImageDigest requires images to use an immutable sha256 digest.
	// +optional
	RequireImageDigest *bool `json:"requireImageDigest,omitempty"`

	// AllowedRegistries restricts image references to these registry or repository prefixes.
	// +optional
	AllowedRegistries []string `json:"allowedRegistries,omitempty"`

	// ExcludedNamespaces list namespaces to bypass this baseline
	// +optional
	ExcludedNamespaces []string `json:"excludedNamespaces,omitempty"`
}

// SecurityBaselineStatus defines the observed state of SecurityBaseline.
type SecurityBaselineStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the SecurityBaseline resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SecurityBaseline is the Schema for the securitybaselines API
type SecurityBaseline struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of SecurityBaseline
	// +required
	Spec SecurityBaselineSpec `json:"spec"`

	// status defines the observed state of SecurityBaseline
	// +optional
	Status SecurityBaselineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SecurityBaselineList contains a list of SecurityBaseline
type SecurityBaselineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecurityBaseline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecurityBaseline{}, &SecurityBaselineList{})
}
