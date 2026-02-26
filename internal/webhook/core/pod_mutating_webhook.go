package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
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
// nolint:gocyclo
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
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

	sort.Slice(policies.Items, func(i, j int) bool {
		return policies.Items[i].Spec.Priority > policies.Items[j].Spec.Priority
	})

	mutated := false

	// Apply TelemetryProfiles
	var telemetryProfiles platformv1alpha1.TelemetryProfileList
	if err := m.Client.List(ctx, &telemetryProfiles, client.InNamespace(req.Namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	sort.Slice(telemetryProfiles.Items, func(i, j int) bool {
		return telemetryProfiles.Items[i].Spec.Priority > telemetryProfiles.Items[j].Spec.Priority
	})

	for _, profile := range telemetryProfiles.Items {
		if profile.Spec.InjectEnvVars && profile.Spec.TracingEndpoint != "" {
			profileMutated := false
			for i := range pod.Spec.Containers {
				// Inject OTEL_EXPORTER_OTLP_ENDPOINT
				hasEndpoint := false
				for _, env := range pod.Spec.Containers[i].Env {
					if env.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
						hasEndpoint = true
						break
					}
				}
				if !hasEndpoint {
					pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{
						Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
						Value: profile.Spec.TracingEndpoint,
					})
					mutated = true
					profileMutated = true
				}

				// Inject OTEL_TRACES_SAMPLER_ARG if SamplingRate is set
				if profile.Spec.SamplingRate != "" {
					hasSamplerArg := false
					for _, env := range pod.Spec.Containers[i].Env {
						if env.Name == "OTEL_TRACES_SAMPLER_ARG" {
							hasSamplerArg = true
							break
						}
					}
					if !hasSamplerArg {
						pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{
							Name:  "OTEL_TRACES_SAMPLER_ARG",
							Value: profile.Spec.SamplingRate,
						})
						mutated = true
						profileMutated = true
					}
				}
			}
			if profileMutated {
				m.Recorder.Event(&profile, "Normal", "PodMutated", fmt.Sprintf("Injected telemetry config to Pod %s in namespace %s", pod.Name, pod.Namespace))
			}
		}
	}

	// Apply policies
	for _, policy := range policies.Items {
		policyMutated := false
		// Enforce default labels
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		for lbl, defaultVal := range policy.Spec.MandatoryLabels {
			if _, exists := pod.Labels[lbl]; !exists {
				pod.Labels[lbl] = defaultVal
				mutated = true
				policyMutated = true
			}
		}

		// Enforce default resources
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Resources.Requests == nil {
				pod.Spec.Containers[i].Resources.Requests = make(corev1.ResourceList)
			}
			for rName, rVal := range policy.Spec.DefaultRequests {
				rn := corev1.ResourceName(rName)
				if _, exists := pod.Spec.Containers[i].Resources.Requests[rn]; !exists {
					qty, parseErr := resource.ParseQuantity(rVal)
					if parseErr != nil {
						return admission.Denied(fmt.Sprintf("WorkloadPolicy %s has invalid defaultRequests quantity for %s: %q", policy.Name, rName, rVal))
					}
					pod.Spec.Containers[i].Resources.Requests[rn] = qty
					mutated = true
					policyMutated = true
				}
			}

			if pod.Spec.Containers[i].Resources.Limits == nil {
				pod.Spec.Containers[i].Resources.Limits = make(corev1.ResourceList)
			}
			for rName, rVal := range policy.Spec.DefaultLimits {
				rn := corev1.ResourceName(rName)
				if _, exists := pod.Spec.Containers[i].Resources.Limits[rn]; !exists {
					qty, parseErr := resource.ParseQuantity(rVal)
					if parseErr != nil {
						return admission.Denied(fmt.Sprintf("WorkloadPolicy %s has invalid defaultLimits quantity for %s: %q", policy.Name, rName, rVal))
					}
					pod.Spec.Containers[i].Resources.Limits[rn] = qty
					mutated = true
					policyMutated = true
				}
			}
		}

		if policyMutated {
			m.Recorder.Event(&policy, "Normal", "PodMutated", fmt.Sprintf("Applied workload policy defaults to Pod %s in namespace %s", pod.Name, pod.Namespace))
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

func SetupPodMutatorWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/mutate-core-v1-pod", &webhook.Admission{
		Handler: &PodMutator{
			Client: mgr.GetClient(),
			//nolint:staticcheck // controller-runtime recorder migration pending
			Recorder: mgr.GetEventRecorderFor("pod-mutator-webhook"),
		},
	})
	return nil
}
