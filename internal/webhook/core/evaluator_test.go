package core

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func boolPtr(value bool) *bool { return &value }

func TestPSSRulesEvaluateContainers(t *testing.T) {
	t.Parallel()

	baseline := &platformv1alpha1.SecurityBaseline{}
	baseline.Spec.DisallowPrivilegeEscalation = boolPtr(true)
	baseline.Spec.DisallowPrivileged = boolPtr(true)
	baseline.Spec.RequireDropAllCapabilities = boolPtr(true)
	baseline.Spec.AllowedCapabilities = []corev1.Capability{"NET_BIND_SERVICE"}
	baseline.Spec.RequireSeccompProfile = boolPtr(true)

	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app",
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
			Privileged:               boolPtr(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"NET_BIND_SERVICE"}},
		},
	}}, InitContainers: []corev1.Container{{Name: "init"}}, EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name: "debug",
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(true),
			Privileged:               boolPtr(true),
			Capabilities:             &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}},
		},
	}}}}}

	violations := EvaluateSecurityBaseline(pod, baseline)
	if len(violations) != 8 {
		t.Fatalf("expected 8 violations across app, init, and ephemeral containers; got %d: %#v", len(violations), violations)
	}
	seen := map[string]int{}
	for _, v := range violations {
		seen[v.Rule+":"+v.Container]++
	}
	for _, key := range []string{
		"DisallowPrivilegeEscalation:init", "DisallowPrivilegeEscalation:debug",
		"DisallowPrivileged:debug", "DropAllCapabilities:init", "DropAllCapabilities:debug",
		"SeccompProfile:app", "SeccompProfile:init", "SeccompProfile:debug",
	} {
		if seen[key] == 0 {
			t.Errorf("expected violation %q; got %#v", key, violations)
		}
	}
}

func TestSeccompProfileInheritsPodProfile(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers:      []corev1.Container{{Name: "app"}},
		InitContainers:  []corev1.Container{{Name: "init"}},
	}}
	baseline := &platformv1alpha1.SecurityBaseline{}
	baseline.Spec.RequireSeccompProfile = boolPtr(true)
	if violations := (&SeccompProfileRule{}).Evaluate(pod, baseline); len(violations) != 0 {
		t.Fatalf("expected Pod-level seccomp profile to be inherited; got %#v", violations)
	}
}
