package core

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

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

	var denialReasons []string
	var warningMessages []string

	for _, baseline := range baselines.Items {
		if slices.Contains(baseline.Spec.ExcludedNamespaces, req.Namespace) {
			continue
		}

		action := baseline.Spec.EnforcementAction
		if action == "" {
			action = platformv1alpha1.EnforcementActionEnforce
		}

		violations := EvaluateSecurityBaseline(pod, &baseline)
		if len(violations) == 0 {
			continue
		}

		for _, violation := range violations {
			detailMsg := fmt.Sprintf("[%s / %s] %s (policy: %s)", violation.Rule, violation.ControlISO, violation.Message, baseline.Name)

			switch action {
			case platformv1alpha1.EnforcementActionEnforce:
				denialReasons = append(denialReasons, detailMsg)
				v.Recorder.Event(&baseline, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
			case platformv1alpha1.EnforcementActionWarn:
				warningMessages = append(warningMessages, detailMsg)
				v.Recorder.Event(&baseline, "Warning", "PolicyViolationWarn", fmt.Sprintf("Warning for Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
			case platformv1alpha1.EnforcementActionAudit:
				podlog.Info("Audit violation detected", "policy", baseline.Name, "pod", pod.Name, "namespace", pod.Namespace, "rule", violation.Rule, "iso", violation.ControlISO, "container", violation.Container)
				v.Recorder.Event(&baseline, "Warning", "PolicyViolationAudit", fmt.Sprintf("Audit violation on Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
			}
		}
	}

	if len(denialReasons) > 0 {
		return admission.Denied(fmt.Sprintf("Pod violates SecurityBaseline: %s", strings.Join(denialReasons, "; ")))
	}

	if len(warningMessages) > 0 {
		return admission.Allowed("").WithWarnings(warningMessages...)
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
