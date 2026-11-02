package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

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

// +kubebuilder:webhook:path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,timeoutSeconds=5,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

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

	// Defaults apply when a Pod is created. Almost all of a running Pod's spec is
	// immutable, so a patch on UPDATE would be rejected by the API server and
	// would block benign updates such as relabelling.
	if req.Operation == admissionv1.Update {
		return admission.Allowed("mutation applies on create only")
	}
	if isMirrorPod(pod, req) {
		return admission.Allowed("mirror Pod of a static Pod")
	}
	dryRun := isDryRun(req)

	podlog.Info("Mutating Pod", "name", pod.Name, "namespace", pod.Namespace)

	defaulter := Defaulter{Client: m.Client}
	changes, err := defaulter.Apply(ctx, req.Namespace, pod, policy.SubjectForPod(pod))
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	for _, change := range changes {
		recordEvent(m.Recorder, dryRun, change.Source, "Normal", "PodMutated",
			fmt.Sprintf("%s to Pod %s in namespace %s", change.Description, pod.Name, pod.Namespace))
	}
	if len(changes) == 0 {
		return admission.Allowed("No mutations applied")
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
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
