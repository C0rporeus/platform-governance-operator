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

package core

import (
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// mirrorPodAnnotation marks the API-server mirror of a static Pod.
	mirrorPodAnnotation = "kubernetes.io/config.mirror"
	nodeUserPrefix      = "system:node:"
	nodesGroup          = "system:nodes"

	seccompPodAnnotation        = "seccomp.security.alpha.kubernetes.io/pod"
	seccompContainerAnnotation  = "container.seccomp.security.alpha.kubernetes.io/"
	apparmorContainerAnnotation = "container.apparmor.security.beta.kubernetes.io/"
)

// isMirrorPod reports whether the request creates or updates the mirror of a
// static Pod. The annotation alone is not trusted: any user can set it, which
// would be a bypass of every baseline. The request must also come from a node
// identity, which is how the kubelet writes mirror Pods.
func isMirrorPod(pod *corev1.Pod, req admission.Request) bool {
	if _, ok := pod.Annotations[mirrorPodAnnotation]; !ok {
		return false
	}
	if !strings.HasPrefix(req.UserInfo.Username, nodeUserPrefix) {
		return false
	}
	return slices.Contains(req.UserInfo.Groups, nodesGroup)
}

// isWindowsPod reports whether the Pod targets Windows nodes. The Pod Security
// Standards relax the privilege-escalation, seccomp and capabilities controls
// for these Pods, and a read-only root filesystem is not supported there.
func isWindowsPod(pod *corev1.Pod) bool {
	return pod.Spec.OS != nil && pod.Spec.OS.Name == corev1.Windows
}

// policyRelevantView is what a policy check can depend on in an update:
// the spec without the fields Pod Security admission exempts, plus the
// seccomp and AppArmor annotations, which it does not exempt.
type policyRelevantView struct {
	Spec        corev1.PodSpec
	Annotations map[string]string
}

func relevantView(pod *corev1.Pod) policyRelevantView {
	spec := pod.Spec.DeepCopy()
	spec.ActiveDeadlineSeconds = nil
	spec.Tolerations = nil

	annotations := map[string]string{}
	for key, value := range pod.Annotations {
		if key == seccompPodAnnotation ||
			strings.HasPrefix(key, seccompContainerAnnotation) ||
			strings.HasPrefix(key, apparmorContainerAnnotation) {
			annotations[key] = value
		}
	}
	return policyRelevantView{Spec: *spec, Annotations: annotations}
}

// isExemptPodUpdate reports whether an update changes only fields that Pod
// Security admission exempts from policy checks (metadata other than the
// seccomp and AppArmor annotations, activeDeadlineSeconds and tolerations).
// Such an update must not be denied even if the Pod already violates a policy,
// otherwise a running Pod could not be labelled or annotated.
func isExemptPodUpdate(oldPod, newPod *corev1.Pod) bool {
	return apiequality.Semantic.DeepEqual(relevantView(oldPod), relevantView(newPod))
}

// isDryRun reports whether the request is a dry run.
func isDryRun(req admission.Request) bool {
	return req.DryRun != nil && *req.DryRun
}

// recordEvent emits an event unless the request is a dry run: a webhook that
// declares sideEffects None must not write objects for requests that are not
// persisted.
func recordEvent(recorder record.EventRecorder, dryRun bool, object runtime.Object, eventType, reason, message string) {
	if dryRun || recorder == nil {
		return
	}
	recorder.Event(object, eventType, reason, message)
}
