package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Fidelity is how faithfully the early validation reproduces what Pod admission will see (ADR-0004).
type Fidelity string

const (
	// FidelityTemplate evaluates the Pod template with this operator's defaults (default, no extra privileges).
	FidelityTemplate Fidelity = "template"
	// FidelityDryRun also sends a synthetic Pod through the whole admission chain as a server-side dry run.
	FidelityDryRun Fidelity = "dry-run"
)

// ParseFidelity validates the --early-validation-fidelity flag value.
func ParseFidelity(value string) (Fidelity, error) {
	switch f := Fidelity(strings.ToLower(strings.TrimSpace(value))); f {
	case FidelityTemplate, FidelityDryRun:
		return f, nil
	default:
		return "", fmt.Errorf("invalid early validation fidelity %q: use template or dry-run", value)
	}
}

const (
	// SafeguardName is the ValidatingAdmissionPolicy (and binding) that limits the operator's
	// identity to dry-run Pod creation. config/dry-run installs it.
	SafeguardName = "platform-governance-operator-dryrun-only"

	// DefaultDryRunTimeout stays under the webhook timeout (3s) so a slow chain falls back to the template result.
	DefaultDryRunTimeout = 2 * time.Second

	ownPodWebhook      = "vpod.kb.io"
	ownDenialPrefix    = "Pod violates SecurityBaseline: "
	webhookDeniedMark  = "denied the request: "
	missingPermission  = `cannot create resource "pods"`
	syntheticHashLabel = "pod-template-hash"
	syntheticHash      = "6d5f8c7b9"
	syntheticUID       = types.UID("00000000-0000-0000-0000-000000000000")
)

// DryRunVerdict is what the admission chain said about the synthetic Pod.
type DryRunVerdict struct {
	// Warnings returned by the chain (this operator's Warn baselines, Pod Security Admission, other webhooks).
	Warnings []string
	// OwnDenial is the rejection by this operator's Pod webhook, without its prefix; empty if none.
	OwnDenial string
	// OtherDenial is a rejection by anyone else (policy engines, quotas, Pod Security Admission); empty if none.
	OtherDenial string
}

// PodDryRunner sends a Pod through the admission chain without persisting it.
type PodDryRunner interface {
	DryRun(ctx context.Context, pod *corev1.Pod) (DryRunVerdict, error)
}

// ErrDryRunUnavailable means the dry run could not give a verdict; callers fall back to the template result.
var ErrDryRunUnavailable = errors.New("dry run unavailable")

// APIDryRunner is the PodDryRunner backed by the Kubernetes API.
type APIDryRunner struct {
	client client.Client
}

// NewAPIDryRunner builds a dedicated client so the API warnings of each request can be captured.
func NewAPIDryRunner(cfg *rest.Config, opts client.Options) (*APIDryRunner, error) {
	dedicated := rest.CopyConfig(cfg)
	dedicated.WarningHandlerWithContext = warningSink{}
	c, err := client.New(dedicated, opts)
	if err != nil {
		return nil, err
	}
	return &APIDryRunner{client: c}, nil
}

type ctxKey struct{}

// warningSink collects the warnings of the requests whose context carries a collector.
type warningSink struct{}

type collector struct {
	mu    sync.Mutex
	items []string
}

func (warningSink) HandleWarningHeaderWithContext(ctx context.Context, code int, _ string, text string) {
	if c, ok := ctx.Value(ctxKey{}).(*collector); ok && code == 299 && text != "" {
		c.mu.Lock()
		c.items = append(c.items, text)
		c.mu.Unlock()
	}
}

// DryRun is the only place in the operator that creates a Pod, and it always sets DryRunAll:
// the operator's RBAC allows creating Pods and the safeguard policy denies it unless the
// request is a dry run (ADR-0004). test: TestPodsAreOnlyCreatedAsDryRuns.
func (r *APIDryRunner) DryRun(ctx context.Context, pod *corev1.Pod) (DryRunVerdict, error) {
	got := &collector{}
	err := r.client.Create(context.WithValue(ctx, ctxKey{}, got), pod, client.DryRunAll)
	return classifyDryRun(got.items, err)
}

// classifyDryRun turns the result of the dry run into a verdict, or ErrDryRunUnavailable.
func classifyDryRun(warnings []string, err error) (DryRunVerdict, error) {
	verdict := DryRunVerdict{Warnings: warnings}
	if err == nil {
		return verdict, nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, missingPermission):
		return DryRunVerdict{}, fmt.Errorf("%w: the operator may not create Pods (is config/dry-run installed?): %v", ErrDryRunUnavailable, err)
	case apierrors.IsForbidden(err) && strings.Contains(message, `admission webhook "`+ownPodWebhook+`"`):
		_, denial, _ := strings.Cut(message, webhookDeniedMark)
		verdict.OwnDenial = strings.TrimPrefix(denial, ownDenialPrefix)
		return verdict, nil
	case apierrors.IsForbidden(err):
		// Pod Security Admission, quotas, other webhooks' denials.
		verdict.OtherDenial = message
		return verdict, nil
	default:
		// Timeouts, 5xx, webhooks without dry-run support, invalid Pods: no verdict.
		return DryRunVerdict{}, fmt.Errorf("%w: %v", ErrDryRunUnavailable, err)
	}
}

// syntheticPod builds the Pod the workload's controller would create, so policy targeting
// and the labels the controllers add resolve as they will for the real Pod. The Pod is not
// defaulted: the real mutating webhooks (this operator's and everyone else's) must run.
func syntheticPod(current template, namespace string) *corev1.Pod {
	pod := podFromTemplate(current.pod, namespace)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	apps := "apps/v1"
	owner := metav1.OwnerReference{APIVersion: apps, Kind: current.kind, Name: current.name, UID: syntheticUID}
	switch current.kind {
	case "Deployment":
		// Deployment -> ReplicaSet (<deployment>-<hash>) -> Pod, labelled with the hash.
		pod.Labels[syntheticHashLabel] = syntheticHash
		owner = metav1.OwnerReference{APIVersion: apps, Kind: "ReplicaSet", Name: current.name + "-" + syntheticHash, UID: syntheticUID}
		pod.GenerateName = current.name + "-" + syntheticHash + "-"
	case "StatefulSet":
		pod.Labels["statefulset.kubernetes.io/pod-name"] = current.name + "-0"
		pod.Labels["controller-revision-hash"] = current.name + "-" + syntheticHash
		pod.Name = current.name + "-0"
	case "DaemonSet":
		pod.Labels["controller-revision-hash"] = syntheticHash
		pod.Labels["pod-template-generation"] = "1"
		pod.GenerateName = current.name + "-"
	case kindJob:
		owner.APIVersion = "batch/v1"
		pod.Labels["job-name"] = current.name
		pod.GenerateName = current.name + "-"
	case "CronJob":
		// CronJob -> Job (<cronjob>-<timestamp>) -> Pod.
		jobName := current.name + "-29000000"
		owner = metav1.OwnerReference{APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: syntheticUID}
		pod.Labels["job-name"] = jobName
		pod.GenerateName = jobName + "-"
	}
	if pod.Name == "" && pod.GenerateName == "" {
		pod.GenerateName = "early-validation-"
	}
	pod.OwnerReferences = []metav1.OwnerReference{owner}
	return pod
}

// SafeguardInstalled reports whether the policy and binding that confine the operator's
// Pod creation to dry runs exist. The dry-run fidelity is only enabled when they do.
func SafeguardInstalled(ctx context.Context, reader client.Reader) (bool, string) {
	var policy admissionregistrationv1.ValidatingAdmissionPolicy
	if err := reader.Get(ctx, client.ObjectKey{Name: SafeguardName}, &policy); err != nil {
		return false, fmt.Sprintf("ValidatingAdmissionPolicy %q cannot be read: %v", SafeguardName, err)
	}
	var binding admissionregistrationv1.ValidatingAdmissionPolicyBinding
	if err := reader.Get(ctx, client.ObjectKey{Name: SafeguardName}, &binding); err != nil {
		return false, fmt.Sprintf("ValidatingAdmissionPolicyBinding %q cannot be read: %v", SafeguardName, err)
	}
	if binding.Spec.PolicyName != SafeguardName {
		return false, "the binding does not reference the safeguard policy"
	}
	denies := false
	for _, action := range binding.Spec.ValidationActions {
		denies = denies || action == admissionregistrationv1.Deny
	}
	if !denies {
		return false, "the binding does not Deny"
	}
	return true, ""
}
