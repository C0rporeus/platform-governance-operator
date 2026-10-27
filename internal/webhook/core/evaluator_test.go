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

func TestHostIsolationRules(t *testing.T) {
	t.Parallel()
	baseline := &platformv1alpha1.SecurityBaseline{}
	baseline.Spec.DisallowHostNamespaces = boolPtr(true)
	baseline.Spec.DisallowHostPathVolumes = boolPtr(true)
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		HostNetwork: true,
		HostPID:     true,
		HostIPC:     true,
		Volumes: []corev1.Volume{
			{Name: "host-config", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc"}}},
			{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}},
		},
	}}
	violations := EvaluateSecurityBaseline(pod, baseline)
	if len(violations) != 4 {
		t.Fatalf("expected hostNetwork, hostPID, hostIPC, and hostPath violations; got %#v", violations)
	}
}

func TestSupplyChainRule(t *testing.T) {
	t.Parallel()
	validDigest := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name          string
		image         string
		allowed       []string
		requireDigest bool
		violations    int
	}{
		{name: "tagged and approved", image: "registry.example.com/team/app:v1", allowed: []string{"registry.example.com/team"}},
		{name: "latest tag", image: "registry.example.com/team/app:latest", allowed: []string{"registry.example.com/team"}, violations: 1},
		{name: "untagged image", image: "registry.example.com/team/app", allowed: []string{"registry.example.com/team"}, violations: 1},
		{name: "unapproved repository prefix", image: "registry.example.com.evil/team/app:v1", allowed: []string{"registry.example.com/team"}, violations: 1},
		{name: "approved digest", image: "registry.example.com/team/app@" + validDigest, allowed: []string{"registry.example.com/team"}, requireDigest: true},
		{name: "tag without required digest", image: "registry.example.com/team/app:v1", allowed: []string{"registry.example.com/team"}, requireDigest: true, violations: 1},
		{name: "invalid digest", image: "registry.example.com/team/app@sha256:bad", allowed: []string{"registry.example.com/team"}, violations: 1},
		{name: "port and tag", image: "registry.example.com:5000/team/app:v1", allowed: []string{"registry.example.com:5000/team"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseline := &platformv1alpha1.SecurityBaseline{}
			baseline.Spec.DisallowLatestTag = boolPtr(true)
			baseline.Spec.RequireImageDigest = boolPtr(tt.requireDigest)
			baseline.Spec.AllowedRegistries = tt.allowed
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: tt.image}}}}
			got := EvaluateSecurityBaseline(pod, baseline)
			if len(got) != tt.violations {
				t.Fatalf("expected %d violations, got %d: %#v", tt.violations, len(got), got)
			}
		})
	}
}

func TestParseImageReference(t *testing.T) {
	t.Parallel()
	ref, err := parseImageReference("registry.example.com:5000/team/app:v2")
	if err != nil {
		t.Fatalf("expected image reference to parse: %v", err)
	}
	if ref.Registry != "registry.example.com:5000" || ref.Name != "registry.example.com:5000/team/app" || ref.Tag != "v2" {
		t.Fatalf("unexpected parsed image reference: %#v", ref)
	}
}

func BenchmarkSupplyChainRule(b *testing.B) {
	baseline := &platformv1alpha1.SecurityBaseline{}
	baseline.Spec.DisallowLatestTag = boolPtr(true)
	baseline.Spec.RequireImageDigest = boolPtr(true)
	baseline.Spec.AllowedRegistries = []string{"registry.example.com/team"}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Name: "app", Image: "registry.example.com/team/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{Name: "sidecar", Image: "registry.example.com/team/sidecar@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}}}
	rule := &SupplyChainRule{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rule.Evaluate(pod, baseline)
	}
}
