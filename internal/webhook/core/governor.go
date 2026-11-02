package core

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

// Governor selects the security baselines that govern a Pod and evaluates them.
// It is the single evaluation path shared by every admission entry point (the Pod
// webhook today, early workload validation in HU-02.3), so both reach the same
// verdict for the same Pod spec by construction.
type Governor struct {
	Client client.Client
	// OperatorNamespace is the namespace the operator runs in. Cluster-wide
	// baselines never apply there. Empty means unknown (nothing extra excluded).
	OperatorNamespace string
}

// Finding is one rule violation together with the action its baseline requests.
type Finding struct {
	Violation SecurityViolation
	// Action is the baseline's enforcement action; empty is normalised to Enforce.
	Action platformv1alpha1.EnforcementAction
	// Source is the policy object events are recorded against.
	Source client.Object
	// Policy identifies the policy in messages ("name" or "cluster/name").
	Policy string
}

// Outcome groups findings by the enforcement action of the baseline that raised them.
type Outcome struct {
	// Enforce findings reject the request (or are downgraded to warnings by a caller).
	Enforce []Finding
	// Warn findings are admitted with a warning shown to the client.
	Warn []Finding
	// Audit findings are admitted silently and only recorded.
	Audit []Finding
}

// Classify splits findings by enforcement action, keeping their order.
func Classify(findings []Finding) Outcome {
	var out Outcome
	for _, f := range findings {
		switch f.Action {
		case platformv1alpha1.EnforcementActionEnforce:
			out.Enforce = append(out.Enforce, f)
		case platformv1alpha1.EnforcementActionWarn:
			out.Warn = append(out.Warn, f)
		case platformv1alpha1.EnforcementActionAudit:
			out.Audit = append(out.Audit, f)
		}
	}
	return out
}

// Message renders the finding as shown to users in denials, warnings and events.
func (f Finding) Message() string {
	return fmt.Sprintf("[%s / %s] %s (policy: %s)", f.Violation.Rule, f.Violation.ControlISO, f.Violation.Message, f.Policy)
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

// Evaluate returns every violation of the baselines that govern pod in namespace.
// subject is what targetRef/selector are matched against; callers derive it from
// the Pod or from the workload that owns the Pod template. Cluster baselines are
// evaluated before namespaced ones and evaluation is additive.
func (g *Governor) Evaluate(ctx context.Context, namespace string, pod *corev1.Pod, subject policy.Subject) ([]Finding, error) {
	enforced, err := g.applicableBaselines(ctx, namespace, subject)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, eb := range enforced {
		action := eb.baseline.Spec.EnforcementAction
		if action == "" {
			action = platformv1alpha1.EnforcementActionEnforce
		}
		for _, violation := range EvaluateSecurityBaseline(pod, eb.baseline) {
			findings = append(findings, Finding{Violation: violation, Action: action, Source: eb.source, Policy: eb.label})
		}
	}
	return findings, nil
}

// applicableBaselines returns the baselines that govern the subject: cluster-wide
// baselines first, then the namespace's own. Evaluation is additive, so a
// namespaced baseline can never weaken a cluster-wide one.
func (g *Governor) applicableBaselines(ctx context.Context, namespace string, subject policy.Subject) ([]enforcedBaseline, error) {
	enforced, err := g.clusterBaselines(ctx, namespace, subject)
	if err != nil {
		return nil, err
	}

	var baselines platformv1alpha1.SecurityBaselineList
	if err := g.Client.List(ctx, &baselines, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	applicable, selectorErrs := policy.Select(baselines.Items, subject, securityBaselineCandidate)
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

// clusterBaselines selects ClusterSecurityBaselines for the namespace. A
// namespace lookup failure is returned, not ignored: skipping would silently
// drop cluster policy.
func (g *Governor) clusterBaselines(ctx context.Context, namespace string, subject policy.Subject) ([]enforcedBaseline, error) {
	var clusterBaselines platformv1alpha1.ClusterSecurityBaselineList
	if err := g.Client.List(ctx, &clusterBaselines); err != nil {
		return nil, err
	}

	var namespaceLabels map[string]string
	namespaceLoaded := false
	candidates := make([]platformv1alpha1.ClusterSecurityBaseline, 0, len(clusterBaselines.Items))
	for _, cb := range clusterBaselines.Items {
		if policy.IsClusterExcluded(namespace, g.OperatorNamespace, cb.Spec.ExcludedNamespaces) {
			continue
		}
		if cb.Spec.NamespaceSelector != nil {
			if !namespaceLoaded {
				var ns corev1.Namespace
				if err := g.Client.Get(ctx, client.ObjectKey{Name: namespace}, &ns); err != nil {
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

	selected, selectorErrs := policy.Select(candidates, subject, clusterBaselineCandidate)
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
