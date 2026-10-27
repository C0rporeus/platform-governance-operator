package core

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// SecurityViolation represents a specific rule violation detected on a Pod.
type SecurityViolation struct {
	Rule       string
	ControlISO string
	Container  string
	Message    string
}

// RuleEvaluator defines the contract for an individual security rule.
type RuleEvaluator interface {
	Name() string
	ISOControl() string
	Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation
}

// RunAsNonRootRule enforces non-root execution (ISO/IEC 27001 A.8.2).
type RunAsNonRootRule struct{}

// Name returns the identifier of the rule.
func (r *RunAsNonRootRule) Name() string { return "RunAsNonRoot" }

// ISOControl returns the ISO/IEC 27001 control reference.
func (r *RunAsNonRootRule) ISOControl() string { return "ISO/IEC 27001 A.8.2" }

// Evaluate verifies that all containers run as non-root.
func (r *RunAsNonRootRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !baseline.Spec.RunAsNonRoot {
		return nil
	}

	var violations []SecurityViolation
	podLevel := pod.Spec.SecurityContext != nil && pod.Spec.SecurityContext.RunAsNonRoot != nil && *pod.Spec.SecurityContext.RunAsNonRoot

	if len(pod.Spec.Containers) == 0 && !podLevel {
		violations = append(violations, SecurityViolation{
			Rule:       r.Name(),
			ControlISO: r.ISOControl(),
			Container:  "",
			Message:    "Pod must run as non-root",
		})
		return violations
	}

	checkContainer := func(c *corev1.Container, kind string) {
		effective := podLevel
		if c.SecurityContext != nil && c.SecurityContext.RunAsNonRoot != nil {
			effective = *c.SecurityContext.RunAsNonRoot
		}
		if !effective {
			violations = append(violations, SecurityViolation{
				Rule:       r.Name(),
				ControlISO: r.ISOControl(),
				Container:  c.Name,
				Message:    fmt.Sprintf("%s container '%s' must run as non-root", kind, c.Name),
			})
		}
	}

	for i := range pod.Spec.Containers {
		checkContainer(&pod.Spec.Containers[i], "app")
	}
	for i := range pod.Spec.InitContainers {
		checkContainer(&pod.Spec.InitContainers[i], "init")
	}
	for i := range pod.Spec.EphemeralContainers {
		checkContainer((*corev1.Container)(&pod.Spec.EphemeralContainers[i].EphemeralContainerCommon), "ephemeral")
	}

	return violations
}

// ReadOnlyRootFilesystemRule enforces read-only root filesystems (ISO/IEC 27001 A.8.18).
type ReadOnlyRootFilesystemRule struct{}

// Name returns the identifier of the rule.
func (r *ReadOnlyRootFilesystemRule) Name() string { return "ReadOnlyRootFilesystem" }

// ISOControl returns the ISO/IEC 27001 control reference.
func (r *ReadOnlyRootFilesystemRule) ISOControl() string { return "ISO/IEC 27001 A.8.18" }

// Evaluate verifies that all containers have a read-only root filesystem.
func (r *ReadOnlyRootFilesystemRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !baseline.Spec.ReadOnlyRootFilesystem {
		return nil
	}

	var violations []SecurityViolation
	checkContainer := func(c *corev1.Container, kind string) {
		if c.SecurityContext == nil || c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
			violations = append(violations, SecurityViolation{
				Rule:       r.Name(),
				ControlISO: r.ISOControl(),
				Container:  c.Name,
				Message:    fmt.Sprintf("%s container '%s' must have read-only root filesystem", kind, c.Name),
			})
		}
	}

	for i := range pod.Spec.Containers {
		checkContainer(&pod.Spec.Containers[i], "app")
	}
	for i := range pod.Spec.InitContainers {
		checkContainer(&pod.Spec.InitContainers[i], "init")
	}
	for i := range pod.Spec.EphemeralContainers {
		checkContainer((*corev1.Container)(&pod.Spec.EphemeralContainers[i].EphemeralContainerCommon), "ephemeral")
	}

	return violations
}

func forEachContainer(pod *corev1.Pod, fn func(*corev1.Container, string)) {
	for i := range pod.Spec.Containers {
		fn(&pod.Spec.Containers[i], "app")
	}
	for i := range pod.Spec.InitContainers {
		fn(&pod.Spec.InitContainers[i], "init")
	}
	for i := range pod.Spec.EphemeralContainers {
		fn((*corev1.Container)(&pod.Spec.EphemeralContainers[i].EphemeralContainerCommon), "ephemeral")
	}
}

func enabled(value *bool) bool { return value != nil && *value }

func violation(rule RuleEvaluator, container, message string) SecurityViolation {
	return SecurityViolation{Rule: rule.Name(), ControlISO: rule.ISOControl(), Container: container, Message: message}
}

// DisallowPrivilegeEscalationRule requires an explicit false setting on each container.
type DisallowPrivilegeEscalationRule struct{}

func (r *DisallowPrivilegeEscalationRule) Name() string       { return "DisallowPrivilegeEscalation" }
func (r *DisallowPrivilegeEscalationRule) ISOControl() string { return "ISO/IEC 27001 A.8.2" }
func (r *DisallowPrivilegeEscalationRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.DisallowPrivilegeEscalation) {
		return nil
	}
	var out []SecurityViolation
	forEachContainer(pod, func(c *corev1.Container, kind string) {
		if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' must disallow privilege escalation", kind, c.Name)))
		}
	})
	return out
}

// DisallowPrivilegedRule rejects containers running in privileged mode.
type DisallowPrivilegedRule struct{}

func (r *DisallowPrivilegedRule) Name() string       { return "DisallowPrivileged" }
func (r *DisallowPrivilegedRule) ISOControl() string { return "ISO/IEC 27001 A.8.2" }
func (r *DisallowPrivilegedRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.DisallowPrivileged) {
		return nil
	}
	var out []SecurityViolation
	forEachContainer(pod, func(c *corev1.Container, kind string) {
		if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' must not be privileged", kind, c.Name)))
		}
	})
	return out
}

// DropAllCapabilitiesRule requires drop=ALL and restricts added capabilities to an allowlist.
type DropAllCapabilitiesRule struct{}

func (r *DropAllCapabilitiesRule) Name() string       { return "DropAllCapabilities" }
func (r *DropAllCapabilitiesRule) ISOControl() string { return "ISO/IEC 27001 A.8.2" }
func (r *DropAllCapabilitiesRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.RequireDropAllCapabilities) {
		return nil
	}
	var out []SecurityViolation
	forEachContainer(pod, func(c *corev1.Container, kind string) {
		if c.SecurityContext == nil || c.SecurityContext.Capabilities == nil || !slices.Contains(c.SecurityContext.Capabilities.Drop, corev1.Capability("ALL")) {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' must drop ALL capabilities", kind, c.Name)))
			return
		}
		for _, cap := range c.SecurityContext.Capabilities.Add {
			if !slices.Contains(baseline.Spec.AllowedCapabilities, cap) {
				out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' adds capability %q which is not allowed", kind, c.Name, cap)))
			}
		}
	})
	return out
}

// SeccompProfileRule requires an allowed profile, inherited from the Pod or set per container.
type SeccompProfileRule struct{}

func (r *SeccompProfileRule) Name() string       { return "SeccompProfile" }
func (r *SeccompProfileRule) ISOControl() string { return "ISO/IEC 27001 A.8.2" }
func validSeccomp(profile *corev1.SeccompProfile) bool {
	return profile != nil && (profile.Type == corev1.SeccompProfileTypeRuntimeDefault || profile.Type == corev1.SeccompProfileTypeLocalhost)
}
func (r *SeccompProfileRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.RequireSeccompProfile) {
		return nil
	}
	podProfile := pod.Spec.SecurityContext != nil && validSeccomp(pod.Spec.SecurityContext.SeccompProfile)
	var out []SecurityViolation
	forEachContainer(pod, func(c *corev1.Container, kind string) {
		containerProfile := c.SecurityContext != nil && validSeccomp(c.SecurityContext.SeccompProfile)
		if !containerProfile && !podProfile {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' must inherit or define RuntimeDefault/Localhost seccomp", kind, c.Name)))
		}
	})
	return out
}

// DisallowHostNamespacesRule rejects Pods that share host namespaces.
type DisallowHostNamespacesRule struct{}

func (r *DisallowHostNamespacesRule) Name() string       { return "DisallowHostNamespaces" }
func (r *DisallowHostNamespacesRule) ISOControl() string { return "ISO/IEC 27001 A.8.18" }
func (r *DisallowHostNamespacesRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.DisallowHostNamespaces) {
		return nil
	}
	var out []SecurityViolation
	for _, namespace := range []struct {
		name  string
		value bool
	}{{"hostNetwork", pod.Spec.HostNetwork}, {"hostPID", pod.Spec.HostPID}, {"hostIPC", pod.Spec.HostIPC}} {
		if namespace.value {
			out = append(out, violation(r, "", fmt.Sprintf("Pod must not enable %s", namespace.name)))
		}
	}
	return out
}

// DisallowHostPathRule rejects hostPath volumes.
type DisallowHostPathRule struct{}

func (r *DisallowHostPathRule) Name() string       { return "DisallowHostPath" }
func (r *DisallowHostPathRule) ISOControl() string { return "ISO/IEC 27001 A.8.18" }
func (r *DisallowHostPathRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	if !enabled(baseline.Spec.DisallowHostPathVolumes) {
		return nil
	}
	var out []SecurityViolation
	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath != nil {
			out = append(out, violation(r, "", fmt.Sprintf("Pod volume '%s' must not use hostPath", volume.Name)))
		}
	}
	return out
}

type imageReference struct {
	Name     string
	Registry string
	Tag      string
	Digest   string
}

var (
	imageTagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	imageDigestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[A-Za-z0-9=_-]+$`)
	sha256Pattern      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func parseImageReference(raw string) (imageReference, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, " \t\n") {
		return imageReference{}, fmt.Errorf("image reference is empty or contains whitespace")
	}
	if strings.Count(raw, "@") > 1 {
		return imageReference{}, fmt.Errorf("image reference contains more than one digest separator")
	}
	nameAndTag, digest, hasDigest := strings.Cut(raw, "@")
	if hasDigest && (!imageDigestPattern.MatchString(digest) || strings.HasPrefix(digest, "sha256:") && !sha256Pattern.MatchString(digest)) {
		return imageReference{}, fmt.Errorf("image reference has an invalid digest")
	}
	if nameAndTag == "" || strings.ContainsAny(nameAndTag, "?#") {
		return imageReference{}, fmt.Errorf("image reference has an invalid name")
	}

	lastSlash := strings.LastIndex(nameAndTag, "/")
	lastColon := strings.LastIndex(nameAndTag, ":")
	name, tag := nameAndTag, ""
	if lastColon > lastSlash {
		name, tag = nameAndTag[:lastColon], nameAndTag[lastColon+1:]
		if !imageTagPattern.MatchString(tag) {
			return imageReference{}, fmt.Errorf("image reference has an invalid tag")
		}
	}
	if name == "" || name != strings.ToLower(name) || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return imageReference{}, fmt.Errorf("image reference has an invalid name")
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._-/:", char)) {
			return imageReference{}, fmt.Errorf("image reference has an invalid name")
		}
	}

	first := strings.SplitN(name, "/", 2)[0]
	registry := "docker.io"
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		registry = first
	}
	return imageReference{Name: name, Registry: registry, Tag: tag, Digest: digest}, nil
}

// SupplyChainRule checks image tags, immutable digests, and allowed registry prefixes.
type SupplyChainRule struct{}

func (r *SupplyChainRule) Name() string       { return "SupplyChain" }
func (r *SupplyChainRule) ISOControl() string { return "ISO/IEC 27001 A.8.30" }
func registryPrefixAllowed(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		if strings.HasSuffix(prefix, "/") || len(name) == len(prefix) || name[len(prefix)] == '/' {
			return true
		}
	}
	return false
}
func (r *SupplyChainRule) Evaluate(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	disallowLatest := enabled(baseline.Spec.DisallowLatestTag)
	requireDigest := enabled(baseline.Spec.RequireImageDigest)
	if !disallowLatest && !requireDigest && len(baseline.Spec.AllowedRegistries) == 0 {
		return nil
	}
	var out []SecurityViolation
	forEachContainer(pod, func(c *corev1.Container, kind string) {
		ref, err := parseImageReference(c.Image)
		if err != nil {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' has invalid image reference %q", kind, c.Name, c.Image)))
			return
		}
		if disallowLatest && (ref.Tag == "latest" || ref.Tag == "" && ref.Digest == "") {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' image must use a non-latest tag or digest", kind, c.Name)))
		}
		if requireDigest && !sha256Pattern.MatchString(ref.Digest) {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' image must use an immutable sha256 digest", kind, c.Name)))
		}
		if len(baseline.Spec.AllowedRegistries) > 0 && !registryPrefixAllowed(ref.Name, baseline.Spec.AllowedRegistries) {
			out = append(out, violation(r, c.Name, fmt.Sprintf("%s container '%s' image registry/repository is not in the allowed prefixes", kind, c.Name)))
		}
	})
	return out
}

// DefaultRules returns the active rule evaluators for SecurityBaseline.
func DefaultRules() []RuleEvaluator {
	return []RuleEvaluator{
		&RunAsNonRootRule{},
		&ReadOnlyRootFilesystemRule{},
		&DisallowPrivilegeEscalationRule{},
		&DisallowPrivilegedRule{},
		&DropAllCapabilitiesRule{},
		&SeccompProfileRule{},
		&DisallowHostNamespacesRule{},
		&DisallowHostPathRule{},
		&SupplyChainRule{},
	}
}

// EvaluateSecurityBaseline evaluates all registered rules against a Pod.
func EvaluateSecurityBaseline(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline) []SecurityViolation {
	return EvaluateSecurityBaselineWithRules(pod, baseline, DefaultRules())
}

// EvaluateSecurityBaselineWithRules allows evaluating a specific set of rules (useful for testing and extension).
func EvaluateSecurityBaselineWithRules(pod *corev1.Pod, baseline *platformv1alpha1.SecurityBaseline, rules []RuleEvaluator) []SecurityViolation {
	var allViolations []SecurityViolation
	for _, rule := range rules {
		violations := rule.Evaluate(pod, baseline)
		if len(violations) > 0 {
			allViolations = append(allViolations, violations...)
		}
	}
	return allViolations
}
