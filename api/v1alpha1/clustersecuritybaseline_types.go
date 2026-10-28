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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterSecurityBaselineSpec defines the desired state of ClusterSecurityBaseline.
// It carries every rule of SecurityBaselineSpec and adds namespace scoping.
// excludedNamespaces (inherited) lists namespaces that bypass the baseline.
// targetRef is not supported at cluster scope; selector matches Pod labels.
// +kubebuilder:validation:XValidation:rule="!has(self.targetRef)",message="targetRef is not supported on a ClusterSecurityBaseline; use selector or namespaceSelector"
type ClusterSecurityBaselineSpec struct {
	SecurityBaselineSpec `json:",inline"`

	// NamespaceSelector limits the baseline to namespaces whose labels match.
	// When omitted the baseline applies to every namespace that is not excluded.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// ClusterSecurityBaselineStatus defines the observed state of ClusterSecurityBaseline.
type ClusterSecurityBaselineStatus struct {
	// conditions represent the current state of the ClusterSecurityBaseline resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterSecurityBaseline is a cluster-wide SecurityBaseline that applies to Pods
// in every non-excluded namespace and cannot be weakened by namespaced baselines.
type ClusterSecurityBaseline struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of ClusterSecurityBaseline
	// +required
	Spec ClusterSecurityBaselineSpec `json:"spec"`

	// status defines the observed state of ClusterSecurityBaseline
	// +optional
	Status ClusterSecurityBaselineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterSecurityBaselineList contains a list of ClusterSecurityBaseline
type ClusterSecurityBaselineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterSecurityBaseline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterSecurityBaseline{}, &ClusterSecurityBaselineList{})
}
