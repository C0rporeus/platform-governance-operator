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

// WorkloadPolicySpec defines the desired state of WorkloadPolicy
type WorkloadPolicySpec struct {
	// DefaultRequests defines the default resource requests applied to containers
	// +optional
	DefaultRequests map[string]string `json:"defaultRequests,omitempty"`

	// DefaultLimits defines the default resource limits applied to containers
	// +optional
	DefaultLimits map[string]string `json:"defaultLimits,omitempty"`

	// MandatoryLabels defines a map of labels and their default values that must be present on workloads
	// +optional
	MandatoryLabels map[string]string `json:"mandatoryLabels,omitempty"`

	// Priority determines the precedence of the policy when multiple apply.
	// Higher numbers indicate higher priority.
	// +kubebuilder:default=0
	// +optional
	Priority int32 `json:"priority,omitempty"`

	// HorizontalScaling defines default Horizontal Pod Autoscaler (HPA) behavior
	// for Deployment workloads governed by this policy.
	// +optional
	HorizontalScaling *HorizontalScalingPolicy `json:"horizontalScaling,omitempty"`
}

const (
	// DefaultHPAMinReplicas is the default minimum replicas for generated HPAs.
	DefaultHPAMinReplicas int32 = 2
	// DefaultHPAMaxReplicas is the default maximum replicas for generated HPAs.
	DefaultHPAMaxReplicas int32 = 10
	// DefaultHPATargetCPU is the default CPU utilization target for generated HPAs.
	DefaultHPATargetCPU int32 = 80
)

// HorizontalScalingPolicy defines default HPA behavior for workloads.
type HorizontalScalingPolicy struct {
	// EnabledByDefault indicates whether HPA should be created for workloads
	// unless explicitly overridden by annotation.
	// +kubebuilder:default=false
	// +optional
	EnabledByDefault bool `json:"enabledByDefault,omitempty"`

	// MinReplicas is the default minimum number of replicas for generated HPAs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	// +optional
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the default maximum number of replicas for generated HPAs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=10
	// +optional
	MaxReplicas int32 `json:"maxReplicas,omitempty"`

	// TargetCPUUtilizationPercentage is the default CPU utilization target for
	// generated HPAs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=80
	// +optional
	TargetCPUUtilizationPercentage int32 `json:"targetCPUUtilizationPercentage,omitempty"`
}

// WorkloadPolicyStatus defines the observed state of WorkloadPolicy.
type WorkloadPolicyStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the WorkloadPolicy resource.
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

// WorkloadPolicy is the Schema for the workloadpolicies API
type WorkloadPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of WorkloadPolicy
	// +required
	Spec WorkloadPolicySpec `json:"spec"`

	// status defines the observed state of WorkloadPolicy
	// +optional
	Status WorkloadPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadPolicyList contains a list of WorkloadPolicy
type WorkloadPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadPolicy{}, &WorkloadPolicyList{})
}
