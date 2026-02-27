package core

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var podlog = logf.Log.WithName("pod-webhook")

// PodValidator validates Pods against SecurityBaselines
type PodValidator struct {
	Client   client.Client
	Recorder record.EventRecorder
	decoder  admission.Decoder
}

// InjectDecoder injects the decoder for admission requests.
func (v *PodValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}

// +kubebuilder:webhook:path=/validate-core-v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=vpod.kb.io,admissionReviewVersions=v1

// Handle validates an incoming Pod admission request against all SecurityBaselines
// active in the request namespace. Returns Denied if any baseline rule is violated.
func (v *PodValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if v.decoder == nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("admission decoder is not initialized"))
	}

	pod := &corev1.Pod{}
	err := v.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	podlog.Info("Validating Pod", "name", pod.Name, "namespace", pod.Namespace)

	// Fetch SecurityBaselines to enforce rules
	var baselines platformv1alpha1.SecurityBaselineList
	if err := v.Client.List(ctx, &baselines, client.InNamespace(req.Namespace)); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	for _, baseline := range baselines.Items {
		if slices.Contains(baseline.Spec.ExcludedNamespaces, req.Namespace) {
			continue
		}

		if baseline.Spec.RunAsNonRoot {
			if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
				msg := "Pod violates SecurityBaseline: must run as non-root"
				v.Recorder.Event(&baseline, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, msg))
				return admission.Denied(msg)
			}
		}

		if baseline.Spec.ReadOnlyRootFilesystem {
			for _, c := range pod.Spec.Containers {
				if c.SecurityContext == nil || c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
					msg := "Pod violates SecurityBaseline: all containers must have read-only root filesystem"
					v.Recorder.Event(&baseline, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, msg))
					return admission.Denied(msg)
				}
			}
			for _, c := range pod.Spec.InitContainers {
				if c.SecurityContext == nil || c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
					msg := "Pod violates SecurityBaseline: all init containers must have read-only root filesystem"
					v.Recorder.Event(&baseline, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, msg))
					return admission.Denied(msg)
				}
			}
		}
	}

	return admission.Allowed("")
}

// SetupPodWebhookWithManager registers the Pod validating webhook with the Manager.
// Uses imperative registration (mgr.GetWebhookServer().Register) because core/v1
// types are not CRDs and cannot use the kubebuilder declarative webhook builder.
func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	handler := &PodValidator{
		Client: mgr.GetClient(),
		//nolint:staticcheck // controller-runtime recorder migration pending
		Recorder: mgr.GetEventRecorderFor("pod-validator-webhook"),
		decoder:  admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register("/validate-core-v1-pod", &webhook.Admission{
		Handler: handler,
	})
	return nil
}
