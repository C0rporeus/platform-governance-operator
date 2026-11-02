package core

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

// Defaulter applies the defaults of the WorkloadPolicies and TelemetryProfiles that
// govern a Pod: mandatory labels, resource requests and limits, and the OpenTelemetry
// variables.
//
// It is the single implementation of those defaults. The Pod mutating webhook applies
// them to the real Pod; the early workload validation applies them to the Pod template
// first, so that it evaluates the Pod as it will look after mutation. Pod admission
// validates the mutated Pod, and the early check must see the same one.
type Defaulter struct {
	Client client.Client
}

// DefaultChange records one policy that changed the Pod, so the caller can report it.
type DefaultChange struct {
	// Source is the policy or profile that changed the Pod.
	Source client.Object
	// Description says what was done, without naming the Pod.
	Description string
}

// Apply mutates pod with the defaults that govern it in namespace. subject decides which
// policies apply (see policy.Select). It returns the changes made, empty when the Pod
// already had everything.
func (d *Defaulter) Apply(ctx context.Context, namespace string, pod *corev1.Pod, subject policy.Subject) ([]DefaultChange, error) {
	var policies platformv1alpha1.WorkloadPolicyList
	if err := d.Client.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	var profiles platformv1alpha1.TelemetryProfileList
	if err := d.Client.List(ctx, &profiles, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	applicablePolicies, selectorErrs := policy.Select(policies.Items, subject, workloadPolicyCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping WorkloadPolicy with invalid selector", "namespace", namespace)
	}
	applicableProfiles, selectorErrs := policy.Select(profiles.Items, subject, telemetryProfileCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping TelemetryProfile with invalid selector", "namespace", namespace)
	}

	changes := applyTelemetry(pod, applicableProfiles)
	for i := range applicablePolicies {
		wp := &applicablePolicies[i]
		changed := applyPolicyLabels(pod, wp)

		resourcesChanged, err := applyPolicyResources(pod, wp)
		if err != nil {
			// Keep the labels: they are valid and already on the Pod, and reporting
			// them is what makes the mutating webhook send them.
			podlog.Error(err, "Skipping resource defaults from WorkloadPolicy due to invalid configuration",
				"policy", wp.Name, "namespace", wp.Namespace)
		}
		if changed || resourcesChanged {
			changes = append(changes, DefaultChange{Source: wp, Description: "Applied workload policy defaults"})
		}
	}
	return changes, nil
}

func workloadPolicyCandidate(p platformv1alpha1.WorkloadPolicy) policy.Candidate {
	return policy.Candidate{Name: p.Name, Namespace: p.Namespace, Priority: p.Spec.Priority, Targeting: p.Spec.PolicyTargeting}
}

func telemetryProfileCandidate(p platformv1alpha1.TelemetryProfile) policy.Candidate {
	return policy.Candidate{Name: p.Name, Namespace: p.Namespace, Priority: p.Spec.Priority, Targeting: p.Spec.PolicyTargeting}
}

// applyTelemetry injects the OpenTelemetry variables of each profile into the app
// containers (not the init containers) that do not define them.
func applyTelemetry(pod *corev1.Pod, profiles []platformv1alpha1.TelemetryProfile) []DefaultChange {
	var changes []DefaultChange
	for i := range profiles {
		profile := &profiles[i]
		if !profile.Spec.InjectEnvVars || profile.Spec.TracingEndpoint == "" {
			continue
		}

		changed := false
		for c := range pod.Spec.Containers {
			container := &pod.Spec.Containers[c]
			if !containerHasEnvVar(container.Env, "OTEL_EXPORTER_OTLP_ENDPOINT") {
				container.Env = append(container.Env, corev1.EnvVar{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: profile.Spec.TracingEndpoint,
				})
				changed = true
			}
			if profile.Spec.SamplingRate != "" && !containerHasEnvVar(container.Env, "OTEL_TRACES_SAMPLER_ARG") {
				container.Env = append(container.Env, corev1.EnvVar{
					Name:  "OTEL_TRACES_SAMPLER_ARG",
					Value: profile.Spec.SamplingRate,
				})
				changed = true
			}
		}
		if changed {
			changes = append(changes, DefaultChange{Source: profile, Description: "Injected telemetry config"})
		}
	}
	return changes
}

func applyPolicyLabels(pod *corev1.Pod, wp *platformv1alpha1.WorkloadPolicy) bool {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}

	changed := false
	for lbl, defaultVal := range wp.Spec.MandatoryLabels {
		if _, exists := pod.Labels[lbl]; !exists {
			pod.Labels[lbl] = string(defaultVal)
			changed = true
		}
	}
	return changed
}

func applyPolicyResources(pod *corev1.Pod, wp *platformv1alpha1.WorkloadPolicy) (bool, error) {
	changed := false
	for i := range pod.Spec.Containers {
		resources := &pod.Spec.Containers[i].Resources
		if resources.Requests == nil {
			resources.Requests = make(corev1.ResourceList)
		}
		for rName, rVal := range wp.Spec.DefaultRequests {
			rn := corev1.ResourceName(rName)
			if _, exists := resources.Requests[rn]; exists {
				continue
			}
			qty, err := resource.ParseQuantity(string(rVal))
			if err != nil {
				return changed, fmt.Errorf("WorkloadPolicy %s has invalid defaultRequests quantity for %s: %q", wp.Name, rName, rVal)
			}
			resources.Requests[rn] = qty
			changed = true
		}

		if resources.Limits == nil {
			resources.Limits = make(corev1.ResourceList)
		}
		for rName, rVal := range wp.Spec.DefaultLimits {
			rn := corev1.ResourceName(rName)
			if _, exists := resources.Limits[rn]; exists {
				continue
			}
			qty, err := resource.ParseQuantity(string(rVal))
			if err != nil {
				return changed, fmt.Errorf("WorkloadPolicy %s has invalid defaultLimits quantity for %s: %q", wp.Name, rName, rVal)
			}
			resources.Limits[rn] = qty
			changed = true
		}
	}
	return changed, nil
}

func containerHasEnvVar(envs []corev1.EnvVar, key string) bool {
	for _, env := range envs {
		if env.Name == key {
			return true
		}
	}
	return false
}
