package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

// PodMutator mutates Pods based on WorkloadPolicy
type PodMutator struct {
	Client   client.Client
	Recorder record.EventRecorder
	decoder  admission.Decoder
}

// InjectDecoder injects the decoder for admission requests.
func (m *PodMutator) InjectDecoder(d admission.Decoder) error {
	m.decoder = d
	return nil
}

// +kubebuilder:webhook:path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

// Handle mutates an incoming Pod admission request by applying defaults from
// WorkloadPolicy (labels, resource requests/limits) and injecting telemetry
// environment variables from TelemetryProfile resources active in the namespace.
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if m.decoder == nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("admission decoder is not initialized"))
	}

	pod := &corev1.Pod{}
	err := m.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	podlog.Info("Mutating Pod", "name", pod.Name, "namespace", pod.Namespace)

	var policies platformv1alpha1.WorkloadPolicyList
	if err := m.Client.List(ctx, &policies, client.InNamespace(req.Namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	subject := policy.SubjectForPod(pod)
	applicablePolicies, selectorErrs := policy.Select(policies.Items, subject, workloadPolicyCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping WorkloadPolicy with invalid selector", "namespace", req.Namespace)
	}

	mutated := false

	// Apply TelemetryProfiles
	var telemetryProfiles platformv1alpha1.TelemetryProfileList
	if err := m.Client.List(ctx, &telemetryProfiles, client.InNamespace(req.Namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	applicableProfiles, selectorErrs := policy.Select(telemetryProfiles.Items, subject, telemetryProfileCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping TelemetryProfile with invalid selector", "namespace", req.Namespace)
	}

	telemetryMutated := m.applyTelemetry(pod, applicableProfiles)
	if telemetryMutated {
		mutated = true
	}

	// Apply policies
	for _, wp := range applicablePolicies {
		policyMutated := m.applyPolicyLabels(pod, &wp)

		resourcesMutated, err := m.applyPolicyResources(pod, &wp)
		if err != nil {
			podlog.Error(err, "Skipping resource defaults from WorkloadPolicy due to invalid configuration",
				"policy", wp.Name, "namespace", wp.Namespace)
			continue
		}
		policyMutated = policyMutated || resourcesMutated

		if policyMutated {
			mutated = true
			m.Recorder.Event(&wp, "Normal", "PodMutated", fmt.Sprintf("Applied workload policy defaults to Pod %s in namespace %s", pod.Name, pod.Namespace))
		}
	}

	if !mutated {
		return admission.Allowed("No mutations applied")
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func workloadPolicyCandidate(p platformv1alpha1.WorkloadPolicy) policy.Candidate {
	return policy.Candidate{Name: p.Name, Namespace: p.Namespace, Priority: p.Spec.Priority, Targeting: p.Spec.PolicyTargeting}
}

func telemetryProfileCandidate(p platformv1alpha1.TelemetryProfile) policy.Candidate {
	return policy.Candidate{Name: p.Name, Namespace: p.Namespace, Priority: p.Spec.Priority, Targeting: p.Spec.PolicyTargeting}
}

func (m *PodMutator) applyTelemetry(pod *corev1.Pod, profiles []platformv1alpha1.TelemetryProfile) bool {
	mutated := false
	for _, profile := range profiles {
		if !profile.Spec.InjectEnvVars || profile.Spec.TracingEndpoint == "" {
			continue
		}

		profileMutated := false
		for i := range pod.Spec.Containers {
			if !containerHasEnvVar(pod.Spec.Containers[i].Env, "OTEL_EXPORTER_OTLP_ENDPOINT") {
				pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{
					Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
					Value: profile.Spec.TracingEndpoint,
				})
				profileMutated = true
			}

			if profile.Spec.SamplingRate != "" && !containerHasEnvVar(pod.Spec.Containers[i].Env, "OTEL_TRACES_SAMPLER_ARG") {
				pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{
					Name:  "OTEL_TRACES_SAMPLER_ARG",
					Value: profile.Spec.SamplingRate,
				})
				profileMutated = true
			}
		}

		if profileMutated {
			mutated = true
			m.Recorder.Event(&profile, "Normal", "PodMutated", fmt.Sprintf("Injected telemetry config to Pod %s in namespace %s", pod.Name, pod.Namespace))
		}
	}
	return mutated
}

func (m *PodMutator) applyPolicyLabels(pod *corev1.Pod, wp *platformv1alpha1.WorkloadPolicy) bool {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}

	mutated := false
	for lbl, defaultVal := range wp.Spec.MandatoryLabels {
		if _, exists := pod.Labels[lbl]; !exists {
			pod.Labels[lbl] = defaultVal
			mutated = true
		}
	}

	return mutated
}

func (m *PodMutator) applyPolicyResources(pod *corev1.Pod, wp *platformv1alpha1.WorkloadPolicy) (bool, error) {
	mutated := false
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Resources.Requests == nil {
			pod.Spec.Containers[i].Resources.Requests = make(corev1.ResourceList)
		}
		for rName, rVal := range wp.Spec.DefaultRequests {
			rn := corev1.ResourceName(rName)
			if _, exists := pod.Spec.Containers[i].Resources.Requests[rn]; exists {
				continue
			}

			qty, parseErr := resource.ParseQuantity(rVal)
			if parseErr != nil {
				return false, fmt.Errorf(
					"WorkloadPolicy %s has invalid defaultRequests quantity for %s: %q",
					wp.Name,
					rName,
					rVal,
				)
			}
			pod.Spec.Containers[i].Resources.Requests[rn] = qty
			mutated = true
		}

		if pod.Spec.Containers[i].Resources.Limits == nil {
			pod.Spec.Containers[i].Resources.Limits = make(corev1.ResourceList)
		}
		for rName, rVal := range wp.Spec.DefaultLimits {
			rn := corev1.ResourceName(rName)
			if _, exists := pod.Spec.Containers[i].Resources.Limits[rn]; exists {
				continue
			}

			qty, parseErr := resource.ParseQuantity(rVal)
			if parseErr != nil {
				return false, fmt.Errorf(
					"WorkloadPolicy %s has invalid defaultLimits quantity for %s: %q",
					wp.Name,
					rName,
					rVal,
				)
			}
			pod.Spec.Containers[i].Resources.Limits[rn] = qty
			mutated = true
		}
	}
	return mutated, nil
}

func containerHasEnvVar(envs []corev1.EnvVar, key string) bool {
	for _, env := range envs {
		if env.Name == key {
			return true
		}
	}
	return false
}

// SetupPodMutatorWebhookWithManager registers the Pod mutating webhook with the Manager.
// Uses imperative registration (mgr.GetWebhookServer().Register) because core/v1
// types are not CRDs and cannot use the kubebuilder declarative webhook builder.
func SetupPodMutatorWebhookWithManager(mgr ctrl.Manager) error {
	handler := &PodMutator{
		Client: mgr.GetClient(),
		//nolint:staticcheck // controller-runtime recorder migration pending
		Recorder: mgr.GetEventRecorderFor("pod-mutator-webhook"),
		decoder:  admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register("/mutate-core-v1-pod", &webhook.Admission{
		Handler: handler,
	})
	return nil
}
