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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

const (
	appsGroup       = "apps"
	kindDeployment  = "Deployment"
	kindReplicaSet  = "ReplicaSet"
	kindPod         = "Pod"
	podTemplateHash = "pod-template-hash"
)

// SubjectForPod builds the Subject used to match policies against a Pod.
// Refs contain the Pod, each direct owner and, for ReplicaSet owners created by
// a Deployment, the Deployment itself. The Deployment name is derived from the
// ReplicaSet name only when it ends with the Pod's pod-template-hash label, so
// no API lookup is needed; if that check fails no Deployment ref is produced.
func SubjectForPod(pod *corev1.Pod) Subject {
	refs := make([]corev1alpha1.PolicyTargetReference, 0, 1+2*len(pod.OwnerReferences))
	if pod.Name != "" {
		refs = append(refs, corev1alpha1.PolicyTargetReference{Kind: kindPod, Name: pod.Name})
	}
	for _, owner := range pod.OwnerReferences {
		gv, err := schema.ParseGroupVersion(owner.APIVersion)
		if err != nil {
			continue
		}
		refs = append(refs, corev1alpha1.PolicyTargetReference{Group: gv.Group, Kind: owner.Kind, Name: owner.Name})
		if gv.Group == appsGroup && owner.Kind == kindReplicaSet {
			if name, ok := deploymentNameFromReplicaSet(owner.Name, pod.Labels[podTemplateHash]); ok {
				refs = append(refs, corev1alpha1.PolicyTargetReference{Group: appsGroup, Kind: kindDeployment, Name: name})
			}
		}
	}
	return Subject{Refs: refs, Labels: pod.Labels}
}

// SubjectForDeployment builds the Subject used to match policies against a
// Deployment. Selectors are evaluated against the pod template labels so a
// policy selects the same workload whether it acts on the Deployment or its Pods.
func SubjectForDeployment(d *appsv1.Deployment) Subject {
	return Subject{
		Refs:   []corev1alpha1.PolicyTargetReference{{Group: appsGroup, Kind: kindDeployment, Name: d.Name}},
		Labels: d.Spec.Template.Labels,
	}
}

func deploymentNameFromReplicaSet(replicaSet, hash string) (string, bool) {
	suffix := "-" + hash
	if hash == "" || !strings.HasSuffix(replicaSet, suffix) || len(replicaSet) <= len(suffix) {
		return "", false
	}
	return strings.TrimSuffix(replicaSet, suffix), true
}
