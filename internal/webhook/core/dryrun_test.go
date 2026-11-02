package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/f3nr1r/platform-governance-operator/internal/policy"
)

type fakeRunner struct {
	verdict DryRunVerdict
	err     error
	calls   int
	pod     *corev1.Pod
}

func (f *fakeRunner) DryRun(_ context.Context, pod *corev1.Pod) (DryRunVerdict, error) {
	f.calls++
	f.pod = pod
	return f.verdict, f.err
}

func compliantSpec() corev1.PodSpec {
	return corev1.PodSpec{Containers: []corev1.Container{{
		Name: "app", Image: "app:1", SecurityContext: &corev1.SecurityContext{RunAsNonRoot: ptr.To(true)},
	}}}
}

// What a sidecar injector would make the real Pod violate and the template does not.
const sidecarDenial = "[RunAsNonRoot / ISO/IEC 27001 A.8.2] app container 'istio-init' must run as non-root (policy: strict)"

func handleWith(t *testing.T, mode EarlyValidationMode, runner *fakeRunner, spec corev1.PodSpec) admission.Response {
	t.Helper()
	v := newWorkloadValidator(t, mode, record.NewFakeRecorder(10), rootBaseline())
	v.DryRun = runner
	return v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", spec), workloadRequestOpts{}))
}

func TestDryRunSeesWhatAMutatorAddedAndWarnModeStillNeverRejects(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{verdict: DryRunVerdict{OwnDenial: sidecarDenial}}

	resp := handleWith(t, EarlyValidationWarn, runner, compliantSpec())

	if !resp.Allowed {
		t.Fatalf("warn mode must never reject, got %v", resp.Result)
	}
	if !hasWarningContaining(resp, "istio-init") || !hasWarningContaining(resp, "after admission mutation") {
		t.Fatalf("the sidecar violation must be reported, got %v", resp.Warnings)
	}
}

func TestDryRunRejectsInEnforceModeOnlyForOurOwnDenial(t *testing.T) {
	t.Parallel()
	resp := handleWith(t, EarlyValidationEnforce, &fakeRunner{verdict: DryRunVerdict{OwnDenial: sidecarDenial}}, compliantSpec())
	if resp.Allowed || !strings.Contains(resp.Result.Message, "istio-init") {
		t.Fatalf("our own denial of the mutated Pod must reject in enforce mode, got %+v", resp)
	}
}

func TestDryRunNeverRejectsBecauseOfThirdParties(t *testing.T) {
	t.Parallel()
	for _, mode := range []EarlyValidationMode{EarlyValidationWarn, EarlyValidationEnforce} {
		runner := &fakeRunner{verdict: DryRunVerdict{
			OtherDenial: `pods "x" is forbidden: violates PodSecurity "restricted:latest"`,
			Warnings:    []string{"would exceed the quota"},
		}}
		resp := handleWith(t, mode, runner, compliantSpec())
		if !resp.Allowed {
			t.Fatalf("%s: a third party's verdict must never reject, got %v", mode, resp.Result)
		}
		if !hasWarningContaining(resp, "PodSecurity") || !hasWarningContaining(resp, "[admission] would exceed the quota") {
			t.Fatalf("%s: third-party verdicts must be forwarded as warnings, got %v", mode, resp.Warnings)
		}
	}
}

func TestDryRunFailureFallsBackToTheTemplateResult(t *testing.T) {
	t.Parallel()
	for _, err := range []error{context.DeadlineExceeded, fmt.Errorf("%w: boom", ErrDryRunUnavailable), errors.New("connection refused")} {
		runner := &fakeRunner{err: err}
		// Compliant template: nothing to say, and the failure must not invent anything or reject.
		resp := handleWith(t, EarlyValidationEnforce, runner, compliantSpec())
		if !resp.Allowed || len(resp.Warnings) != 0 || runner.calls != 1 {
			t.Fatalf("%v: expected the quiet template result, got %+v (calls %d)", err, resp, runner.calls)
		}
		// Violating template: the template findings still reject.
		resp = handleWith(t, EarlyValidationEnforce, &fakeRunner{err: err}, violatingPod().Spec)
		if resp.Allowed {
			t.Fatalf("%v: a failed dry run must not hide template violations", err)
		}
	}
}

func TestDryRunIgnoresAVerdictThatComesWithAnError(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{verdict: DryRunVerdict{OwnDenial: sidecarDenial}, err: context.DeadlineExceeded}
	resp := handleWith(t, EarlyValidationEnforce, runner, compliantSpec())
	if !resp.Allowed || len(resp.Warnings) != 0 {
		t.Fatalf("an incomplete dry run must not reject or warn, got %+v", resp)
	}
}

func TestDryRunDoesNotRepeatWhatTheTemplateAlreadyReported(t *testing.T) {
	t.Parallel()
	// Warn mode: the template violates, and the real chain says the same thing.
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(10), rootBaseline())
	findingMessage := ""
	probe := &fakeRunner{}
	v.DryRun = probe
	first := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))
	if len(first.Warnings) == 0 {
		t.Fatal("expected the template warning")
	}
	findingMessage = strings.TrimPrefix(first.Warnings[0], earlyPrefix+" ")

	probe.verdict = DryRunVerdict{OwnDenial: findingMessage, Warnings: []string{findingMessage}}
	second := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))
	if strings.Join(second.Warnings, "|") != strings.Join(first.Warnings, "|") {
		t.Fatalf("the dry run repeated a known violation:\nfirst  %v\nsecond %v", first.Warnings, second.Warnings)
	}
}

func TestDryRunIsSkippedWhenTheTemplateIsAlreadyRejected(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	resp := handleWith(t, EarlyValidationEnforce, runner, violatingPod().Spec)
	if resp.Allowed || runner.calls != 0 {
		t.Fatalf("a rejected template needs no dry run (calls %d, allowed %v)", runner.calls, resp.Allowed)
	}
}

func TestDryRunIsNotUsedWhenThePodTemplateDidNotChange(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{verdict: DryRunVerdict{OwnDenial: sidecarDenial}}
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10), rootBaseline())
	v.DryRun = runner
	d := deploymentOf("web", compliantSpec())
	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Update, "Deployment", d, workloadRequestOpts{old: d}))
	if !resp.Allowed || runner.calls != 0 {
		t.Fatalf("an unchanged template must not be evaluated (D3), calls %d", runner.calls)
	}
}

func TestDryRunWarningsStayInsideTheAPIServerBudget(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 600)
	warnings := []string{long + "1", long + "2", long + "3", long + "4", long + "5"}
	runner := &fakeRunner{verdict: DryRunVerdict{OwnDenial: sidecarDenial + "; " + long, OtherDenial: long, Warnings: warnings}}
	resp := handleWith(t, EarlyValidationWarn, runner, manyContainersViolating(40))

	total := 0
	for _, w := range resp.Warnings {
		if n := utf8.RuneCountInString(w); n > 256 {
			t.Errorf("a warning has %d runes, the API server cuts at 256", n)
		}
		total += utf8.RuneCountInString(w)
	}
	if total > 4096 {
		t.Fatalf("warnings total %d runes, the API server drops anything past 4096", total)
	}
}

func manyContainersViolating(n int) corev1.PodSpec {
	spec := corev1.PodSpec{}
	for i := range n {
		spec.Containers = append(spec.Containers, corev1.Container{Name: fmt.Sprintf("c%d", i), Image: "x"})
	}
	return spec
}

func TestClassifyDryRun(t *testing.T) {
	t.Parallel()
	forbidden := func(msg string) error {
		return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "x", errors.New(msg))
	}
	own := `admission webhook "vpod.kb.io" denied the request: Pod violates SecurityBaseline: ` + sidecarDenial
	cases := []struct {
		name        string
		warnings    []string
		err         error
		wantErr     bool
		own, other  bool
		wantWarning int
	}{
		{name: "accepted", warnings: []string{"w"}, wantWarning: 1},
		{name: "own denial", err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "x", errors.New(own)), own: true},
		{name: "own denial wording of the real API server", err: &apierrors.StatusError{ErrStatus: metav1.Status{
			Status: metav1.StatusFailure, Code: 403, Reason: metav1.StatusReasonForbidden, Message: own}}, own: true},
		{name: "pod security admission", err: forbidden(`violates PodSecurity "restricted:latest"`), other: true},
		{name: "quota", err: forbidden("exceeded quota: q"), other: true},
		{name: "other webhook", err: &apierrors.StatusError{ErrStatus: metav1.Status{Code: 403, Reason: metav1.StatusReasonForbidden,
			Message: `admission webhook "x.example.com" denied the request: nope`}}, other: true},
		{name: "operator lacks create pods", err: forbidden(`User "system:serviceaccount:a:b" cannot create resource "pods" in API group ""`), wantErr: true},
		{name: "safeguard-less install: still no verdict", err: apierrors.NewServiceUnavailable("down"), wantErr: true},
		{name: "webhook without dry run support", err: apierrors.NewBadRequest("does not support dry run"), wantErr: true},
		{name: "timeout", err: context.DeadlineExceeded, wantErr: true},
		{name: "invalid pod", err: apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "x", nil), wantErr: true},
	}
	for _, c := range cases {
		verdict, err := classifyDryRun(c.warnings, c.err)
		if c.wantErr {
			if !errors.Is(err, ErrDryRunUnavailable) {
				t.Errorf("%s: expected ErrDryRunUnavailable, got %v", c.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if (verdict.OwnDenial != "") != c.own || (verdict.OtherDenial != "") != c.other || len(verdict.Warnings) != c.wantWarning {
			t.Errorf("%s: got %+v", c.name, verdict)
		}
		if c.own && verdict.OwnDenial != sidecarDenial {
			t.Errorf("%s: the denial must be returned without the webhook prefix, got %q", c.name, verdict.OwnDenial)
		}
	}
}

func TestSyntheticPodResolvesPolicyTargetingLikeTheRealPod(t *testing.T) {
	t.Parallel()
	tpl := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}}, Spec: compliantSpec()}
	for _, c := range []struct{ kind, name, group, wantKind, wantName string }{
		{"Deployment", "web", "apps", "Deployment", "web"},
		{"StatefulSet", "db", "apps", "StatefulSet", "db"},
		{"DaemonSet", "agent", "apps", "DaemonSet", "agent"},
		{"Job", "migrate", "batch", "Job", "migrate"},
	} {
		pod := syntheticPod(template{kind: c.kind, name: c.name, pod: tpl}, "team-a")
		found := false
		for _, ref := range policy.SubjectForPod(pod).Refs {
			found = found || (ref.Kind == c.wantKind && ref.Name == c.wantName && ref.Group == c.group)
		}
		if !found {
			t.Errorf("%s: the synthetic Pod must resolve to a %s %q reference, got %+v", c.kind, c.wantKind, c.wantName, policy.SubjectForPod(pod).Refs)
		}
		if pod.Name == "" && pod.GenerateName == "" {
			t.Errorf("%s: the Pod needs a name", c.kind)
		}
	}
	// The Pod carries what the controllers add, and the template is left alone.
	web := syntheticPod(template{kind: "Deployment", name: "web", pod: tpl}, "team-a")
	if web.Labels[syntheticHashLabel] == "" {
		t.Error("a Deployment Pod carries pod-template-hash, as the real one does")
	}
	if _, leaked := tpl.Labels[syntheticHashLabel]; leaked {
		t.Error("building the synthetic Pod must not change the template")
	}
	// It is not defaulted: the real mutators must see the Pod as the controller would create it.
	if web.Spec.Containers[0].Resources.Requests != nil {
		t.Error("the synthetic Pod must not be defaulted")
	}
}

func TestSafeguardInstalled(t *testing.T) {
	t.Parallel()
	scheme := newWebhookTestScheme(t)
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	policyObj := &admissionregistrationv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: SafeguardName}}
	binding := func(policyName string, actions ...admissionregistrationv1.ValidationAction) *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
		return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
			ObjectMeta: metav1.ObjectMeta{Name: SafeguardName},
			Spec:       admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: policyName, ValidationActions: actions},
		}
	}
	deny := admissionregistrationv1.Deny
	cases := []struct {
		name string
		objs []any
		want bool
	}{
		{"nothing installed", nil, false},
		{"policy without binding", []any{policyObj}, false},
		{"binding without policy", []any{binding(SafeguardName, deny)}, false},
		{"binding to another policy", []any{policyObj, binding("other", deny)}, false},
		{"binding that only warns", []any{policyObj, binding(SafeguardName, admissionregistrationv1.Warn)}, false},
		{"complete", []any{policyObj, binding(SafeguardName, deny)}, true},
	}
	for _, c := range cases {
		b := fake.NewClientBuilder().WithScheme(scheme)
		for _, o := range c.objs {
			switch obj := o.(type) {
			case *admissionregistrationv1.ValidatingAdmissionPolicy:
				b = b.WithObjects(obj.DeepCopy())
			case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
				b = b.WithObjects(obj.DeepCopy())
			}
		}
		ok, why := SafeguardInstalled(context.Background(), b.Build())
		if ok != c.want || (!ok && why == "") {
			t.Errorf("%s: got %v %q, want %v with a reason when false", c.name, ok, why, c.want)
		}
	}
}

func TestParseFidelity(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Fidelity{"template": FidelityTemplate, " Dry-Run ": FidelityDryRun} {
		if got, err := ParseFidelity(in); err != nil || got != want {
			t.Errorf("%q: got %q %v", in, got, err)
		}
	}
	if _, err := ParseFidelity("full"); err == nil {
		t.Error("an unknown fidelity must be rejected")
	}
}

// The operator's RBAC allows creating Pods and nothing but the safeguard policy stops a real
// one, so the code must never create a Pod without DryRunAll (ADR-0004, safeguard 3).
func TestPodsAreOnlyCreatedAsDryRuns(t *testing.T) {
	t.Parallel()
	creators := map[string]bool{}
	for _, root := range []string{"../../../cmd", "../../../internal"} {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			raw, _ := os.ReadFile(path)
			for line := range strings.SplitSeq(string(raw), "\n") {
				if strings.Contains(line, ".Create(") {
					creators[filepath.Base(path)] = true
					if filepath.Base(path) == "dryrun.go" && !strings.Contains(line, "client.DryRunAll") {
						t.Errorf("%s: Create without client.DryRunAll: %s", path, strings.TrimSpace(line))
					}
				}
			}
			return nil
		})
	}
	// Only these two files create objects; any new one must be reviewed against the safeguard.
	want := map[string]bool{"dryrun.go": true, "workloadpolicy_controller.go": true}
	if fmt.Sprint(creators) != fmt.Sprint(want) {
		t.Fatalf("files that call Create changed: %v (expected %v); a new creator must be reviewed against ADR-0004", creators, want)
	}
}
