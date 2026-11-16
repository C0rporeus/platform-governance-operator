package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platform "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
	"github.com/f3nr1r/platform-governance-operator/internal/webhook/core"
)

const (
	// AuditInterval schedules another assessment even when the policy has not changed.
	AuditInterval = 5 * time.Minute
	// AuditPodLimit bounds Pod inspections per policy and cycle, including nonmatching Pods.
	AuditPodLimit = 500
	// AuditExampleLimit bounds persisted violation examples.
	AuditExampleLimit = 10
)

// AuditReport describes one bounded assessment. An incomplete report is never proof of compliance.
type AuditReport struct {
	Inspected int
	Evaluated int
	Violating int
	Complete  bool
	Examples  []platform.ComplianceExample
}

// AuditReconciler assesses existing Pods using exactly the admission Governor.
// It reads the manager cache and never mutates workloads or emits admission events.
type AuditReconciler struct {
	Client            client.Client
	OperatorNamespace string
	// MaxPods overrides the default inspection budget when positive.
	MaxPods int
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Assess scans only namespaces in the baseline's scope. Policy watches trigger it
// through the existing baseline controllers; their periodic requeue observes Pod changes.
func (a *AuditReconciler) Assess(ctx context.Context, source client.Object) (AuditReport, error) {
	spec, namespaces, err := a.auditScope(ctx, source)
	if err != nil {
		return AuditReport{}, err
	}
	limit := a.MaxPods
	if limit <= 0 {
		limit = AuditPodLimit
	}
	report := AuditReport{Complete: true}
	slices.Sort(namespaces)
	governor := core.Governor{Client: a.Client, OperatorNamespace: a.OperatorNamespace}
	for _, namespace := range namespaces {
		var pods corev1.PodList
		// controller-runtime's cache does not implement API pagination: the budget
		// bounds inspection/evaluation, not informer storage or List allocation.
		if err := a.Client.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
			return AuditReport{}, fmt.Errorf("list audit Pods in %q: %w", namespace, err)
		}
		sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
		for i := range pods.Items {
			if report.Inspected >= limit {
				report.Complete = false
				return report, nil
			}
			report.Inspected++
			pod := &pods.Items[i]
			matched, err := policy.Matches(policy.Candidate{Targeting: spec.PolicyTargeting}, policy.SubjectForPod(pod))
			if err != nil {
				return AuditReport{}, err
			}
			if !matched {
				continue
			}
			findings, err := governor.Evaluate(ctx, namespace, pod, policy.SubjectForPod(pod))
			if err != nil {
				return AuditReport{}, fmt.Errorf("evaluate audit Pod %s/%s: %w", namespace, pod.Name, err)
			}
			report.Evaluated++
			report.recordFindings(source, pod, findings)
		}
	}
	return report, nil
}

func (r *AuditReport) recordFindings(source client.Object, pod *corev1.Pod, findings []core.Finding) {
	violating := false
	for _, finding := range findings {
		if reflect.TypeOf(finding.Source) != reflect.TypeOf(source) ||
			client.ObjectKeyFromObject(finding.Source) != client.ObjectKeyFromObject(source) {
			continue
		}
		violating = true
		example := platform.ComplianceExample{Namespace: pod.Namespace, Pod: pod.Name, Rule: finding.Violation.Rule}
		if len(r.Examples) < AuditExampleLimit && !slices.Contains(r.Examples, example) {
			r.Examples = append(r.Examples, example)
		}
	}
	if violating {
		r.Violating++
	}
}

func (a *AuditReconciler) auditScope(ctx context.Context, source client.Object) (platform.SecurityBaselineSpec, []string, error) {
	var spec platform.SecurityBaselineSpec
	var selector *metav1.LabelSelector
	switch baseline := source.(type) {
	case *platform.SecurityBaseline:
		spec = baseline.Spec
		if slices.Contains(spec.ExcludedNamespaces, baseline.Namespace) {
			return spec, nil, nil
		}
		if err := policy.ValidateTargeting(spec.PolicyTargeting); err != nil {
			return spec, nil, err
		}
		return spec, []string{baseline.Namespace}, nil
	case *platform.ClusterSecurityBaseline:
		spec, selector = baseline.Spec.SecurityBaselineSpec, baseline.Spec.NamespaceSelector
		if spec.TargetRef != nil {
			return spec, nil, fmt.Errorf("targetRef is not supported on a ClusterSecurityBaseline")
		}
	default:
		return spec, nil, fmt.Errorf("unsupported audit source %T", source)
	}
	if err := policy.ValidateTargeting(spec.PolicyTargeting); err != nil {
		return spec, nil, err
	}
	if _, err := metav1.LabelSelectorAsSelector(selector); err != nil {
		return spec, nil, err
	}
	var namespaces corev1.NamespaceList
	if err := a.Client.List(ctx, &namespaces); err != nil {
		return spec, nil, err
	}
	var scope []string
	for _, namespace := range namespaces.Items {
		if policy.IsClusterExcluded(namespace.Name, a.OperatorNamespace, spec.ExcludedNamespaces) {
			continue
		}
		matches, err := policy.NamespaceMatches(selector, namespace.Labels)
		if err != nil {
			return spec, nil, err
		}
		if matches {
			scope = append(scope, namespace.Name)
		}
	}
	return spec, scope, nil
}
