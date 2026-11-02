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

// PolicyTargetReference identifies a single resource a policy attaches to.
// It follows the Gateway API PolicyAttachment (GEP-713) shape; the target is
// always resolved in the namespace of the policy.
//
// Pods are matched through their owners, so for the API groups whose kinds are known
// the kind must belong to the group: a reference such as kind Deployment without group
// apps would never match anything and would fail silently. Groups that are not listed
// (for example argoproj.io) are accepted as they are.
//
// +kubebuilder:validation:XValidation:rule="(!has(self.group) || size(self.group) == 0) ? self.kind in ['Pod', 'ReplicationController'] : self.group == 'apps' ? self.kind in ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'] : self.group == 'batch' ? self.kind == 'Job' : true",message="kind is not valid for the API group: core is Pod or ReplicationController, apps is Deployment, StatefulSet, DaemonSet or ReplicaSet, batch is Job (a CronJob owns Jobs, not Pods)",reason=FieldValueInvalid,fieldPath=.kind
type PolicyTargetReference struct {
	// Group is the API group of the target. Empty means the core group.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Group string `json:"group,omitempty"`

	// Kind is the kind of the target (for example Deployment or StatefulSet).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Kind string `json:"kind"`

	// Name is the name of the target.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

// PolicyTargeting narrows which workloads a policy applies to.
// When both fields are omitted the policy applies to the whole namespace.
// When both are set, targetRef takes precedence over selector.
type PolicyTargeting struct {
	// TargetRef attaches the policy to one specific resource.
	// +optional
	TargetRef *PolicyTargetReference `json:"targetRef,omitempty"`

	// Selector attaches the policy to workloads whose labels match.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}
