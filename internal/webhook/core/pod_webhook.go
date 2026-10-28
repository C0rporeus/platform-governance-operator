package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// enforcedBaseline is a baseline selected for a Pod, ready to evaluate.
type enforcedBaseline struct {
	// baseline holds the rules; for cluster baselines it is a view of the cluster spec.
	baseline *platformv1alpha1.SecurityBaseline
	// source is the object events are recorded against.
	source client.Object
	// label identifies the policy in messages.
	label string
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

	enforced, err := v.applicableBaselines(ctx, pod, req.Namespace)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	var denialReasons []string
	var warningMessages []string

	for _, eb := range enforced {
		action := eb.baseline.Spec.EnforcementAction
		if action == "" {
			action = platformv1alpha1.EnforcementActionEnforce
		}

		for _, violation := range EvaluateSecurityBaseline(pod, eb.baseline) {
			detailMsg := fmt.Sprintf("[%s / %s] %s (policy: %s)", violation.Rule, violation.ControlISO, violation.Message, eb.label)

			switch action {
			case platformv1alpha1.EnforcementActionEnforce:
				denialReasons = append(denialReasons, detailMsg)
				v.Recorder.Event(eb.source, "Warning", "PodDenied", fmt.Sprintf("Denied Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
			case platformv1alpha1.EnforcementActionWarn:
				warningMessages = append(warningMessages, detailMsg)
				v.Recorder.Event(eb.source, "Warning", "PolicyViolationWarn", fmt.Sprintf("Warning for Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
			case platformv1alpha1.EnforcementActionAudit:
				podlog.Info("Audit violation detected", "policy", eb.label, "pod", pod.Name, "namespace", pod.Namespace, "rule", violation.Rule, "iso", violation.ControlISO, "container", violation.Container)
				v.Recorder.Event(eb.source, "Warning", "PolicyViolationAudit", fmt.Sprintf("Audit violation on Pod %s in namespace %s: %s", pod.Name, pod.Namespace, detailMsg))
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

// applicableBaselines returns the baselines that govern the Pod: cluster-wide
// baselines first, then the namespace's own. Evaluation is additive, so a
// namespaced baseline can never weaken a cluster-wide one.
func (v *PodValidator) applicableBaselines(ctx context.Context, pod *corev1.Pod, namespace string) ([]enforcedBaseline, error) {
	enforced, err := v.clusterBaselines(ctx, pod, namespace)
	if err != nil {
		return nil, err
	}

	var baselines platformv1alpha1.SecurityBaselineList
	if err := v.Client.List(ctx, &baselines, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	applicable, selectorErrs := policy.Select(baselines.Items, policy.SubjectForPod(pod), securityBaselineCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping SecurityBaseline with invalid selector", "namespace", namespace)
	}
	for i := range applicable {
		if slices.Contains(applicable[i].Spec.ExcludedNamespaces, namespace) {
			continue
		}
		enforced = append(enforced, enforcedBaseline{baseline: &applicable[i], source: &applicable[i], label: applicable[i].Name})
	}
	return enforced, nil
}

// clusterBaselines selects ClusterSecurityBaselines for the Pod's namespace. A
// namespace lookup failure is returned, not ignored: skipping would silently
// drop cluster policy.
func (v *PodValidator) clusterBaselines(ctx context.Context, pod *corev1.Pod, namespace string) ([]enforcedBaseline, error) {
	var clusterBaselines platformv1alpha1.ClusterSecurityBaselineList
	if err := v.Client.List(ctx, &clusterBaselines); err != nil {
		return nil, err
	}

	var namespaceLabels map[string]string
	namespaceLoaded := false
	candidates := make([]platformv1alpha1.ClusterSecurityBaseline, 0, len(clusterBaselines.Items))
	for _, cb := range clusterBaselines.Items {
		if policy.IsClusterExcluded(namespace, v.OperatorNamespace, cb.Spec.ExcludedNamespaces) {
			continue
		}
		if cb.Spec.NamespaceSelector != nil {
			if !namespaceLoaded {
				var ns corev1.Namespace
				if err := v.Client.Get(ctx, client.ObjectKey{Name: namespace}, &ns); err != nil {
					return nil, fmt.Errorf("get namespace %q for ClusterSecurityBaseline %q: %w", namespace, cb.Name, err)
				}
				namespaceLabels, namespaceLoaded = ns.Labels, true
			}
			match, err := policy.NamespaceMatches(cb.Spec.NamespaceSelector, namespaceLabels)
			if err != nil {
				podlog.Error(err, "Skipping ClusterSecurityBaseline with invalid namespaceSelector", "policy", cb.Name)
				continue
			}
			if !match {
				continue
			}
		}
		candidates = append(candidates, cb)
	}

	selected, selectorErrs := policy.Select(candidates, policy.SubjectForPod(pod), clusterBaselineCandidate)
	for _, selErr := range selectorErrs {
		podlog.Error(selErr, "Skipping ClusterSecurityBaseline with invalid selector")
	}

	enforced := make([]enforcedBaseline, 0, len(selected))
	for i := range selected {
		enforced = append(enforced, enforcedBaseline{
			baseline: &platformv1alpha1.SecurityBaseline{
				ObjectMeta: metav1.ObjectMeta{Name: selected[i].Name},
				Spec:       selected[i].Spec.SecurityBaselineSpec,
			},
			source: &selected[i],
			label:  "cluster/" + selected[i].Name,
		})
	}
	return enforced, nil
}

func clusterBaselineCandidate(b platformv1alpha1.ClusterSecurityBaseline) policy.Candidate {
	return policy.Candidate{Name: b.Name, ClusterScoped: true, Targeting: b.Spec.PolicyTargeting}
}

func securityBaselineCandidate(b platformv1alpha1.SecurityBaseline) policy.Candidate {
	return policy.Candidate{Name: b.Name, Namespace: b.Namespace, Targeting: b.Spec.PolicyTargeting}
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
