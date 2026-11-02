package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

var podlog = logf.Log.WithName("pod-webhook")

// PodValidator validates Pods against SecurityBaselines
type PodValidator struct {
	Client   client.Client
	Recorder record.EventRecorder
	// OperatorNamespace is the namespace the operator runs in. Cluster-wide
	// baselines never apply there. Empty means unknown (nothing extra excluded).
	OperatorNamespace string
	decoder           admission.Decoder
}

// InjectDecoder injects the decoder for admission requests.
func (v *PodValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}

// +kubebuilder:webhook:path=/validate-core-v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,timeoutSeconds=5,groups="",resources=pods,verbs=create;update,versions=v1,name=vpod.kb.io,admissionReviewVersions=v1

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

	if isMirrorPod(pod, req) {
		return admission.Allowed("mirror Pod of a static Pod")
	}

	if req.Operation == admissionv1.Update {
		oldPod := &corev1.Pod{}
		if err := v.decoder.DecodeRaw(req.OldObject, oldPod); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if isExemptPodUpdate(oldPod, pod) {
			return admission.Allowed("update changes only fields exempt from policy checks")
		}
	}

	podlog.Info("Validating Pod", "name", pod.Name, "namespace", pod.Namespace)
	dryRun := isDryRun(req)

	governor := Governor{Client: v.Client, OperatorNamespace: v.OperatorNamespace}
	findings, err := governor.Evaluate(ctx, req.Namespace, pod, policy.SubjectForPod(pod))
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	var denialReasons []string
	var warningMessages []string

	for _, f := range findings {
		detailMsg := f.Message()
		switch f.Action {
		case platformv1alpha1.EnforcementActionEnforce:
			denialReasons = append(denialReasons, detailMsg)
			recordEvent(v.Recorder, dryRun, f.Source, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
		case platformv1alpha1.EnforcementActionWarn:
			warningMessages = append(warningMessages, detailMsg)
			recordEvent(v.Recorder, dryRun, f.Source, "Warning", "PolicyViolationWarn", fmt.Sprintf("Warning for Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
		case platformv1alpha1.EnforcementActionAudit:
			podlog.Info("Audit violation detected", "policy", f.Policy, "pod", pod.Name, "namespace", pod.Namespace, "rule", f.Violation.Rule, "iso", f.Violation.ControlISO, "container", f.Violation.Container)
			recordEvent(v.Recorder, dryRun, f.Source, "Warning", "PolicyViolationAudit", fmt.Sprintf("Audit violation on Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
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

		OperatorNamespace: os.Getenv("POD_NAMESPACE"),
	}

	mgr.GetWebhookServer().Register("/validate-core-v1-pod", &webhook.Admission{
		Handler: handler,
	})
	return nil
}
