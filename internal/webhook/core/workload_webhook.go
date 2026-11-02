package core

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

var workloadlog = logf.Log.WithName("workload-webhook")

// EarlyValidationMode controls what the early workload validation does with a violation.
type EarlyValidationMode string

const (
	// EarlyValidationOff does not register the webhook.
	EarlyValidationOff EarlyValidationMode = "off"
	// EarlyValidationWarn reports every violation as a warning and never rejects (default).
	EarlyValidationWarn EarlyValidationMode = "warn"
	// EarlyValidationEnforce rejects the workload for the violations Pod admission would reject.
	EarlyValidationEnforce EarlyValidationMode = "enforce"
)

// ParseEarlyValidationMode validates the --early-validation-mode flag value.
func ParseEarlyValidationMode(value string) (EarlyValidationMode, error) {
	switch mode := EarlyValidationMode(strings.ToLower(strings.TrimSpace(value))); mode {
	case EarlyValidationOff, EarlyValidationWarn, EarlyValidationEnforce:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid early validation mode %q: use off, warn or enforce", value)
	}
}

const (
	// earlyPrefix tells the user a message comes from the early check, not from Pod admission.
	earlyPrefix = "[early]"
	// maxEarlyWarnings caps the warnings returned to the client: the API server drops
	// warning headers beyond a few kilobytes, so a long list would be cut silently.
	maxEarlyWarnings = 20

	auditAnnotationViolations = "violations"
	auditAnnotationPolicies   = "policies"
)

// WorkloadValidator gives early feedback on Deployments and StatefulSets: it
// evaluates the Pod template with the same rules Pod admission applies to the Pods
// it will create, so a violation shows up on `kubectl apply` instead of as a
// ReplicaSet that cannot create Pods.
//
// It is feedback, not the security boundary: the webhook fails open (ADR-0003) and
// every Pod is still validated by PodValidator.
type WorkloadValidator struct {
	Client   client.Client
	Recorder record.EventRecorder
	// OperatorNamespace is the namespace the operator runs in (see Governor).
	OperatorNamespace string
	Mode              EarlyValidationMode
	decoder           admission.Decoder
}

// InjectDecoder injects the decoder for admission requests.
func (v *WorkloadValidator) InjectDecoder(d admission.Decoder) error {
	v.decoder = d
	return nil
}

// +kubebuilder:webhook:path=/validate-apps-v1-workload,mutating=false,failurePolicy=ignore,sideEffects=None,timeoutSeconds=3,matchPolicy=Equivalent,groups=apps,resources=deployments;statefulsets,verbs=create;update,versions=v1,name=vworkload.kb.io,admissionReviewVersions=v1

// template is what the early check needs from a workload.
type template struct {
	kind    string
	name    string
	pod     corev1.PodTemplateSpec
	subject policy.Subject
	deleted bool
}

// Handle evaluates the Pod template of the Deployment or StatefulSet in the request.
func (v *WorkloadValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	if v.decoder == nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("admission decoder is not initialized"))
	}
	if v.Mode == EarlyValidationOff || req.Operation == admissionv1.Delete {
		return admission.Allowed("early validation does not apply")
	}

	current, err := v.decode(req.Kind.Kind, req.Object.Raw, req.Namespace)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if current.deleted {
		return admission.Allowed("workload is being deleted")
	}

	// D3: only a change of the Pod template can create Pods that need evaluating. The
	// Deployment controller keeps updating annotations and replicas of workloads that
	// already violate a policy; evaluating those would make them impossible to reconcile.
	if req.Operation == admissionv1.Update {
		previous, err := v.decode(req.Kind.Kind, req.OldObject.Raw, req.Namespace)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if apiequality.Semantic.DeepEqual(previous.pod, current.pod) {
			return admission.Allowed("pod template unchanged")
		}
	}

	pod := podFromTemplate(current.pod, req.Namespace)
	governor := Governor{Client: v.Client, OperatorNamespace: v.OperatorNamespace}
	findings, err := governor.Evaluate(ctx, req.Namespace, pod, current.subject)
	if err != nil {
		// The early check is only feedback and the webhook fails open: do not block
		// the apply because policy could not be read; Pod admission decides later.
		workloadlog.Error(err, "Early validation skipped", "kind", current.kind, "name", current.name, "namespace", req.Namespace)
		return admission.Allowed("early validation skipped: policies could not be read")
	}

	return v.respond(current, req, Classify(findings))
}

// respond turns the classified findings into the admission response for the configured mode.
func (v *WorkloadValidator) respond(current template, req admission.Request, outcome Outcome) admission.Response {
	dryRun := isDryRun(req)
	label := fmt.Sprintf("%s %s/%s", current.kind, req.Namespace, current.name)
	var warnings, denials []string

	for _, f := range outcome.Enforce {
		msg := fmt.Sprintf("%s %s", earlyPrefix, f.Message())
		if v.Mode == EarlyValidationEnforce {
			denials = append(denials, f.Message())
			recordEvent(v.Recorder, dryRun, f.Source, "Warning", "WorkloadDenied",
				fmt.Sprintf("Denied %s: %s", label, f.Message()))
			continue
		}
		warnings = append(warnings, msg)
		recordEvent(v.Recorder, dryRun, f.Source, "Warning", "WorkloadViolationWarn",
			fmt.Sprintf("%s would be denied at Pod admission: %s", label, f.Message()))
	}
	for _, f := range outcome.Warn {
		warnings = append(warnings, fmt.Sprintf("%s %s", earlyPrefix, f.Message()))
		recordEvent(v.Recorder, dryRun, f.Source, "Warning", "WorkloadViolationWarn",
			fmt.Sprintf("Warning for %s: %s", label, f.Message()))
	}
	for _, f := range outcome.Audit {
		workloadlog.Info("Audit violation detected", "policy", f.Policy, "kind", current.kind, "name", current.name,
			"namespace", req.Namespace, "rule", f.Violation.Rule, "iso", f.Violation.ControlISO, "container", f.Violation.Container)
		recordEvent(v.Recorder, dryRun, f.Source, "Warning", "WorkloadViolationAudit",
			fmt.Sprintf("Audit violation on %s: %s", label, f.Message()))
	}

	var response admission.Response
	if len(denials) > 0 {
		response = admission.Denied(fmt.Sprintf("%s %s Pod template violates SecurityBaseline: %s",
			earlyPrefix, label, strings.Join(denials, "; ")))
	} else {
		response = admission.Allowed("")
	}
	if len(warnings) > 0 {
		response = response.WithWarnings(capWarnings(warnings)...)
	}
	response.AuditAnnotations = auditAnnotations(outcome)
	return response
}

// decode reads a Deployment or StatefulSet and extracts what the early check needs.
func (v *WorkloadValidator) decode(kind string, raw []byte, namespace string) (template, error) {
	switch kind {
	case "Deployment":
		d := &appsv1.Deployment{}
		if err := v.decoder.DecodeRaw(runtime.RawExtension{Raw: raw}, d); err != nil {
			return template{}, err
		}
		d.Namespace = namespaceOr(d.Namespace, namespace)
		return template{kind: kind, name: d.Name, pod: d.Spec.Template,
			subject: policy.SubjectForDeployment(d), deleted: isDeleting(d.ObjectMeta)}, nil
	case "StatefulSet":
		s := &appsv1.StatefulSet{}
		if err := v.decoder.DecodeRaw(runtime.RawExtension{Raw: raw}, s); err != nil {
			return template{}, err
		}
		s.Namespace = namespaceOr(s.Namespace, namespace)
		return template{kind: kind, name: s.Name, pod: s.Spec.Template,
			subject: policy.SubjectForStatefulSet(s), deleted: isDeleting(s.ObjectMeta)}, nil
	default:
		return template{}, fmt.Errorf("early validation does not handle kind %q", kind)
	}
}

// podFromTemplate builds the Pod the workload controller will create from the template.
// It has no name or owners: the Subject is built from the workload, not from the Pod.
func podFromTemplate(tpl corev1.PodTemplateSpec, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Labels:      tpl.Labels,
			Annotations: tpl.Annotations,
		},
		Spec: *tpl.Spec.DeepCopy(),
	}
}

func isDeleting(meta metav1.ObjectMeta) bool { return meta.DeletionTimestamp != nil }

func namespaceOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// capWarnings keeps the first maxEarlyWarnings messages and summarises the rest.
func capWarnings(warnings []string) []string {
	if len(warnings) <= maxEarlyWarnings {
		return warnings
	}
	extra := len(warnings) - maxEarlyWarnings
	capped := slices.Clone(warnings[:maxEarlyWarnings])
	return append(capped, fmt.Sprintf("%s ... and %d more violations; Pod admission reports all of them", earlyPrefix, extra))
}

// auditAnnotations records which rules and policies matched in the API server audit
// log (the API server prefixes the keys with the webhook name).
func auditAnnotations(outcome Outcome) map[string]string {
	rules := map[string]struct{}{}
	policies := map[string]struct{}{}
	for _, group := range [][]Finding{outcome.Enforce, outcome.Warn, outcome.Audit} {
		for _, f := range group {
			rules[f.Violation.Rule] = struct{}{}
			policies[f.Policy] = struct{}{}
		}
	}
	if len(rules) == 0 {
		return nil
	}
	return map[string]string{
		auditAnnotationViolations: strings.Join(sortedKeys(rules), ","),
		auditAnnotationPolicies:   strings.Join(sortedKeys(policies), ","),
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SetupWorkloadWebhookWithManager registers the early workload validation with the
// Manager. Imperative registration, like the Pod webhooks: these are core types.
func SetupWorkloadWebhookWithManager(mgr ctrl.Manager, mode EarlyValidationMode) error {
	handler := &WorkloadValidator{
		Client: mgr.GetClient(),
		//nolint:staticcheck // controller-runtime recorder migration pending
		Recorder:          mgr.GetEventRecorderFor("workload-validator-webhook"),
		OperatorNamespace: os.Getenv("POD_NAMESPACE"),
		Mode:              mode,
		decoder:           admission.NewDecoder(mgr.GetScheme()),
	}
	mgr.GetWebhookServer().Register("/validate-apps-v1-workload", &webhook.Admission{Handler: handler})
	return nil
}
