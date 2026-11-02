package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// maxAdmissionLines bounds the lines the dry run adds, so they cannot exhaust the warning budget.
const maxAdmissionLines = 3

// dryRunExtras is what the dry run adds to the template evaluation.
type dryRunExtras struct {
	// denials reject in enforce mode: this operator's own rejection that the template evaluation missed.
	denials []string
	// warnings report what the rest of the admission chain said. They never reject.
	warnings []string
}

// dryRunExtras sends a synthetic Pod through the admission chain and returns only what the
// template evaluation did not already say. Every failure returns nothing: the template
// result stands and the webhook still fails open (ADR-0004, conditions 2 to 5).
func (v *WorkloadValidator) dryRunExtras(ctx context.Context, current template, req admission.Request, findings []Finding, outcome Outcome) dryRunExtras {
	if v.DryRun == nil {
		return dryRunExtras{}
	}
	// A template that is already rejected needs no second opinion.
	if v.Mode == EarlyValidationEnforce && len(outcome.Enforce) > 0 {
		return dryRunExtras{}
	}
	timeout := v.DryRunTimeout
	if timeout <= 0 {
		timeout = DefaultDryRunTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	verdict, err := v.DryRun.DryRun(ctx, syntheticPod(current, req.Namespace))
	if err != nil {
		workloadlog.Info("Dry run gave no verdict; using the template result", "kind", current.kind, "name", current.name,
			"namespace", req.Namespace, "reason", err.Error(), "unavailable", errors.Is(err, ErrDryRunUnavailable))
		return dryRunExtras{}
	}
	return classifyExtras(verdict, findings, v.Mode)
}

// classifyExtras drops what the template evaluation already reported and turns the rest into lines.
func classifyExtras(verdict DryRunVerdict, findings []Finding, mode EarlyValidationMode) dryRunExtras {
	known := make([]string, 0, len(findings))
	for _, f := range findings {
		known = append(known, f.Message())
	}
	var extras dryRunExtras

	if rest := withoutKnown(verdict.OwnDenial, known); rest != "" {
		if mode == EarlyValidationEnforce {
			extras.denials = append(extras.denials, "after admission mutation: "+rest)
		} else {
			extras.warnings = append(extras.warnings, truncateRunes(fmt.Sprintf("%s the Pods would be denied after admission mutation: %s", earlyPrefix, rest), warningLineRunes))
		}
	}
	if verdict.OtherDenial != "" {
		extras.warnings = append(extras.warnings, truncateRunes(fmt.Sprintf("%s the admission chain would reject the Pods: %s", earlyPrefix, verdict.OtherDenial), warningLineRunes))
	}
	var forwarded []string
	for _, w := range verdict.Warnings {
		if slices.Contains(known, w) || slices.Contains(forwarded, w) {
			continue
		}
		forwarded = append(forwarded, w)
	}
	for i, w := range forwarded {
		if len(extras.warnings) >= maxAdmissionLines {
			extras.warnings = append(extras.warnings, fmt.Sprintf("%s ... and %d more admission warnings", earlyPrefix, len(forwarded)-i))
			break
		}
		extras.warnings = append(extras.warnings, truncateRunes(fmt.Sprintf("%s [admission] %s", earlyPrefix, w), warningLineRunes))
	}
	return extras
}

// withoutKnown removes the messages the template evaluation already produced from a denial text.
func withoutKnown(denial string, known []string) string {
	for _, m := range known {
		denial = strings.ReplaceAll(denial, m, "")
	}
	return strings.Trim(denial, "; ")
}
