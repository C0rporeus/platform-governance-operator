/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	jsonpatch "github.com/evanphx/json-patch/v5"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func newWorkloadValidator(
	t *testing.T, mode EarlyValidationMode, recorder record.EventRecorder, objs ...client.Object,
) *WorkloadValidator {
	t.Helper()
	scheme := newWebhookTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &WorkloadValidator{Client: c, Recorder: recorder, Mode: mode, decoder: admission.NewDecoder(scheme)}
}

func deploymentOf(name string, spec corev1.PodSpec) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: spec},
		},
	}
}

func statefulSetOf(name string, spec corev1.PodSpec) *appsv1.StatefulSet {
	labels := map[string]string{"app": name}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: spec},
		},
	}
}

type workloadRequestOpts struct {
	old    any
	dryRun bool
}

func workloadRequest(t *testing.T, op admissionv1.Operation, kind string, obj any, opts workloadRequestOpts) admission.Request {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	r := admissionv1.AdmissionRequest{
		Namespace: testNamespace,
		Operation: op,
		Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: kind},
		Object:    runtime.RawExtension{Raw: raw},
	}
	if opts.old != nil {
		oldRaw, err := json.Marshal(opts.old)
		if err != nil {
			t.Fatal(err)
		}
		r.OldObject = runtime.RawExtension{Raw: oldRaw}
	}
	if opts.dryRun {
		r.DryRun = ptr.To(true)
	}
	return admission.Request{AdmissionRequest: r}
}

func baselineWith(action platformv1alpha1.EnforcementAction) *platformv1alpha1.SecurityBaseline {
	b := rootBaseline()
	b.Spec.EnforcementAction = action
	return b
}

func hasWarningContaining(resp admission.Response, text string) bool {
	return slices.ContainsFunc(resp.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func TestWorkloadValidatorWarnModeReportsViolationsWithoutRejecting(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(10), rootBaseline())

	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))

	if !resp.Allowed {
		t.Fatalf("warn mode must never reject, got %v", resp.Result)
	}
	if !hasWarningContaining(resp, "[early]") || !hasWarningContaining(resp, "RunAsNonRoot") ||
		!hasWarningContaining(resp, "policy: strict") {
		t.Fatalf("expected an [early] warning naming the rule and policy, got %v", resp.Warnings)
	}
	if resp.AuditAnnotations["violations"] != "ReadOnlyRootFilesystem,RunAsNonRoot" &&
		!strings.Contains(resp.AuditAnnotations["violations"], "RunAsNonRoot") {
		t.Fatalf("audit annotations do not name the violated rules: %v", resp.AuditAnnotations)
	}
	if resp.AuditAnnotations["policies"] != "strict" {
		t.Fatalf("audit annotations do not name the policy: %v", resp.AuditAnnotations)
	}
}

func TestWorkloadValidatorEnforceModeRejectsWhatPodAdmissionWouldReject(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10), rootBaseline())

	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))

	if resp.Allowed {
		t.Fatal("enforce mode must reject a template that violates an Enforce baseline")
	}
	message := resp.Result.Message
	for _, want := range []string{"[early]", "Deployment team-a/web", "RunAsNonRoot", "policy: strict"} {
		if !strings.Contains(message, want) {
			t.Errorf("denial %q does not contain %q", message, want)
		}
	}
}

func TestWorkloadValidatorAdmitsCompliantTemplatesQuietly(t *testing.T) {
	t.Parallel()
	for _, mode := range []EarlyValidationMode{EarlyValidationWarn, EarlyValidationEnforce} {
		v := newWorkloadValidator(t, mode, record.NewFakeRecorder(10), rootBaseline())
		compliant := corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true)},
			Containers:      []corev1.Container{{Name: "app", Image: "nginx:1"}},
		}
		resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "StatefulSet",
			statefulSetOf("db", compliant), workloadRequestOpts{}))
		if !resp.Allowed || len(resp.Warnings) != 0 || resp.AuditAnnotations != nil {
			t.Fatalf("%s: a compliant template must be admitted without noise, got %+v", mode, resp)
		}
	}
}

func TestWorkloadValidatorOffModeDoesNothing(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationOff, record.NewFakeRecorder(10), rootBaseline())
	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))
	if !resp.Allowed || len(resp.Warnings) != 0 {
		t.Fatalf("off mode must not evaluate anything, got %+v", resp)
	}
}

func TestWorkloadValidatorHonoursTheActionOfEachBaseline(t *testing.T) {
	t.Parallel()
	web := deploymentOf("web", violatingPod().Spec)

	t.Run("a Warn baseline only warns, even in enforce mode", func(t *testing.T) {
		t.Parallel()
		v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10),
			baselineWith(platformv1alpha1.EnforcementActionWarn))
		resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment", web, workloadRequestOpts{}))
		if !resp.Allowed || !hasWarningContaining(resp, "RunAsNonRoot") {
			t.Fatalf("expected an admitted request with a warning, got %+v", resp)
		}
	})

	t.Run("an Audit baseline is admitted silently but leaves an audit trail", func(t *testing.T) {
		t.Parallel()
		v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10),
			baselineWith(platformv1alpha1.EnforcementActionAudit))
		resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment", web, workloadRequestOpts{}))
		if !resp.Allowed || len(resp.Warnings) != 0 {
			t.Fatalf("expected a silent admission, got %+v", resp)
		}
		if resp.AuditAnnotations["policies"] != "strict" {
			t.Fatalf("expected audit annotations, got %v", resp.AuditAnnotations)
		}
	})
}

// D3: only a change of the Pod template can create Pods that need evaluating.
func TestWorkloadValidatorEvaluatesOnlyWhenThePodTemplateChanges(t *testing.T) {
	t.Parallel()
	legacy := deploymentOf("legacy", violatingPod().Spec)
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10), rootBaseline())
	ctx := context.Background()

	t.Run("scaling and controller annotations on a violating workload stay reconcilable", func(t *testing.T) {
		t.Parallel()
		updated := legacy.DeepCopy()
		updated.Spec.Replicas = ptr.To(int32(5))
		updated.Annotations = map[string]string{"deployment.kubernetes.io/revision": "7"}
		updated.Labels = map[string]string{"owner": "team-a"}
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Update, "Deployment", updated, workloadRequestOpts{old: legacy}))
		if !resp.Allowed || len(resp.Warnings) != 0 {
			t.Fatalf("an update that keeps the Pod template must be admitted untouched, got %+v", resp)
		}
	})

	t.Run("a template change is evaluated", func(t *testing.T) {
		t.Parallel()
		updated := legacy.DeepCopy()
		updated.Spec.Template.Spec.Containers[0].Image = "nginx:2"
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Update, "Deployment", updated, workloadRequestOpts{old: legacy}))
		if resp.Allowed {
			t.Fatal("a changed template that still violates the baseline must be rejected in enforce mode")
		}
	})

	t.Run("a rollout restart changes the template and is evaluated", func(t *testing.T) {
		t.Parallel()
		updated := legacy.DeepCopy()
		updated.Spec.Template.Annotations = map[string]string{"kubectl.kubernetes.io/restartedAt": "2026-11-02T00:00:00Z"}
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Update, "Deployment", updated, workloadRequestOpts{old: legacy}))
		if resp.Allowed {
			t.Fatal("the new Pods of a restart would be denied, so the early check must say so")
		}
	})

	t.Run("deletes and objects being deleted are never evaluated", func(t *testing.T) {
		t.Parallel()
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Delete, "Deployment", legacy, workloadRequestOpts{}))
		if !resp.Allowed {
			t.Fatalf("delete must be admitted, got %+v", resp)
		}
		deleting := legacy.DeepCopy()
		now := metav1.NewTime(time.Now())
		deleting.DeletionTimestamp = &now
		deleting.Finalizers = []string{"example.com/cleanup"}
		resp = v.Handle(ctx, workloadRequest(t, admissionv1.Update, "Deployment", deleting, workloadRequestOpts{old: legacy}))
		if !resp.Allowed {
			t.Fatalf("a workload being deleted must be admitted, got %+v", resp)
		}
	})
}

func TestWorkloadValidatorMatchesTargetingAgainstTheWorkload(t *testing.T) {
	t.Parallel()
	targeted := func(kind, name string) *platformv1alpha1.SecurityBaseline {
		b := rootBaseline()
		b.Name = strings.ToLower(kind) + "-" + name
		b.Spec.TargetRef = &platformv1alpha1.PolicyTargetReference{Group: "apps", Kind: kind, Name: name}
		return b
	}
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10),
		targeted("Deployment", "web"), targeted("StatefulSet", "db"))
	ctx := context.Background()
	spec := violatingPod().Spec

	cases := []struct {
		name     string
		kind     string
		obj      any
		wantDeny bool
	}{
		{"targeted Deployment", "Deployment", deploymentOf("web", spec), true},
		{"another Deployment", "Deployment", deploymentOf("api", spec), false},
		{"targeted StatefulSet", "StatefulSet", statefulSetOf("db", spec), true},
		{"another StatefulSet", "StatefulSet", statefulSetOf("cache", spec), false},
	}
	for _, c := range cases {
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Create, c.kind, c.obj, workloadRequestOpts{}))
		if resp.Allowed == c.wantDeny {
			t.Errorf("%s: allowed = %v, want denied = %v", c.name, resp.Allowed, c.wantDeny)
		}
	}
}

func TestWorkloadValidatorRecordsEventsExceptOnDryRun(t *testing.T) {
	t.Parallel()
	web := deploymentOf("web", violatingPod().Spec)

	t.Run("real requests record an event", func(t *testing.T) {
		t.Parallel()
		recorder := record.NewFakeRecorder(10)
		v := newWorkloadValidator(t, EarlyValidationWarn, recorder, rootBaseline())
		v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment", web, workloadRequestOpts{}))
		select {
		case event := <-recorder.Events:
			if !strings.Contains(event, "WorkloadViolationWarn") || !strings.Contains(event, "Deployment team-a/web") {
				t.Fatalf("unexpected event %q", event)
			}
		default:
			t.Fatal("expected an event")
		}
	})

	t.Run("dry runs warn but write nothing (sideEffects: None)", func(t *testing.T) {
		t.Parallel()
		recorder := record.NewFakeRecorder(10)
		v := newWorkloadValidator(t, EarlyValidationWarn, recorder, rootBaseline())
		resp := v.Handle(context.Background(),
			workloadRequest(t, admissionv1.Create, "Deployment", web, workloadRequestOpts{dryRun: true}))
		if !hasWarningContaining(resp, "RunAsNonRoot") {
			t.Fatalf("a dry run still shows the warnings, got %v", resp.Warnings)
		}
		if len(recorder.Events) != 0 {
			t.Fatalf("a dry run must not record events, got %d", len(recorder.Events))
		}
	})
}

func manyContainers(n int) corev1.PodSpec {
	spec := corev1.PodSpec{}
	for i := range n {
		spec.Containers = append(spec.Containers, corev1.Container{Name: fmt.Sprintf("c%d", i), Image: "nginx:1"})
	}
	return spec
}

// One mistake repeated in many containers is one warning, not one per container.
func TestWorkloadValidatorGroupsTheSameViolationAcrossContainers(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(100), rootBaseline())

	resp := v.Handle(context.Background(),
		workloadRequest(t, admissionv1.Create, "Deployment", deploymentOf("big", manyContainers(25)), workloadRequestOpts{}))

	if len(resp.Warnings) != 1 {
		t.Fatalf("25 containers breaking one rule must produce one warning, got %d: %v", len(resp.Warnings), resp.Warnings)
	}
	warning := resp.Warnings[0]
	for _, want := range []string{"[early]", "RunAsNonRoot", "25 violations", "c0, c1, c2 and 22 more", "policy: strict"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q does not contain %q", warning, want)
		}
	}
}

// The API server accepts 4096 characters of warnings, then truncates each to 256 and
// drops the rest. The report must stay inside that, with every line under 256.
func TestWorkloadValidatorWarningsFitTheAPIServerBudget(t *testing.T) {
	t.Parallel()
	baselines := make([]client.Object, 0, 60)
	for i := range 60 {
		b := rootBaseline()
		b.Name = fmt.Sprintf("policy-with-a-fairly-long-descriptive-name-%02d", i)
		baselines = append(baselines, b)
	}
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(500), baselines...)

	resp := v.Handle(context.Background(),
		workloadRequest(t, admissionv1.Create, "Deployment", deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))

	total := 0
	for _, w := range resp.Warnings {
		runes := utf8.RuneCountInString(w)
		if runes > 255 {
			t.Errorf("a warning of %d characters would be truncated by the API server: %q", runes, w)
		}
		total += runes
	}
	if total > 4096 {
		t.Fatalf("warnings total %d characters; the API server drops everything past 4096", total)
	}
	last := resp.Warnings[len(resp.Warnings)-1]
	if !strings.Contains(last, "more kinds of violation") {
		t.Fatalf("when warnings are left out the last line must say so, got %q", last)
	}
	if len(resp.Warnings) < 10 {
		t.Fatalf("the budget should fit many groups before summarising, got %d lines", len(resp.Warnings))
	}
}

func TestEventsDescribeTheWholeGroupWithoutTheWarningLimit(t *testing.T) {
	t.Parallel()
	recorder := record.NewFakeRecorder(100)
	v := newWorkloadValidator(t, EarlyValidationWarn, recorder, rootBaseline())

	v.Handle(context.Background(),
		workloadRequest(t, admissionv1.Create, "Deployment", deploymentOf("big", manyContainers(8)), workloadRequestOpts{}))

	if len(recorder.Events) != 1 {
		t.Fatalf("one group must be one event, got %d", len(recorder.Events))
	}
	event := <-recorder.Events
	if !strings.Contains(event, "c0, c1, c2, c3, c4, c5, c6, c7") {
		t.Fatalf("the event must name every container, got %q", event)
	}
}

// ADR-0003: the early check is feedback. If policies cannot be read it must not block the apply.
func TestWorkloadValidatorFailsOpenWhenPoliciesCannotBeRead(t *testing.T) {
	t.Parallel()
	scheme := newWebhookTestScheme(t)
	broken := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errors.New("cache not synced")
		},
	}).Build()
	v := &WorkloadValidator{Client: broken, Recorder: record.NewFakeRecorder(10), Mode: EarlyValidationEnforce,
		decoder: admission.NewDecoder(scheme)}

	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
		deploymentOf("web", violatingPod().Spec), workloadRequestOpts{}))

	if !resp.Allowed {
		t.Fatalf("an unreadable policy set must not reject the workload, got %+v", resp)
	}
}

func TestWorkloadValidatorRejectsUnsupportedKinds(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(10))
	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "ReplicaSet",
		&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, workloadRequestOpts{}))
	if resp.Allowed {
		t.Fatal("a kind the webhook is not registered for must be reported, not silently admitted")
	}
}

func TestParseEarlyValidationMode(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]EarlyValidationMode{
		"off": EarlyValidationOff, "warn": EarlyValidationWarn, "ENFORCE": EarlyValidationEnforce, " warn ": EarlyValidationWarn,
	} {
		got, err := ParseEarlyValidationMode(input)
		if err != nil || got != want {
			t.Errorf("ParseEarlyValidationMode(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "block", "true"} {
		if _, err := ParseEarlyValidationMode(bad); err == nil {
			t.Errorf("ParseEarlyValidationMode(%q) must fail", bad)
		}
	}
}

// R-05: the early check and Pod admission must reach the same verdict for the same
// Pod spec. Both go through Governor.Evaluate; this guards against them drifting apart.
func TestEarlyValidationAndPodAdmissionAgree(t *testing.T) {
	t.Parallel()
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	strict := &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "strict", Namespace: testNamespace},
		Spec: platformv1alpha1.SecurityBaselineSpec{
			EnforcementAction:           platformv1alpha1.EnforcementActionEnforce,
			RunAsNonRoot:                true,
			ReadOnlyRootFilesystem:      true,
			DisallowPrivilegeEscalation: ptr.To(true),
			DisallowPrivileged:          ptr.To(true),
			RequireDropAllCapabilities:  ptr.To(true),
			RequireSeccompProfile:       ptr.To(true),
			DisallowHostNamespaces:      ptr.To(true),
			DisallowHostPathVolumes:     ptr.To(true),
			DisallowLatestTag:           ptr.To(true),
			RequireImageDigest:          ptr.To(true),
			AllowedRegistries:           []string{"registry.example.com/"},
		},
	}
	compliant := func() corev1.PodSpec {
		return corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.example.com/app@" + digest,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		}
	}
	mutations := map[string]func(*corev1.PodSpec){
		"compliant":       func(*corev1.PodSpec) {},
		"runs as root":    func(s *corev1.PodSpec) { s.SecurityContext.RunAsNonRoot = ptr.To(false) },
		"writable rootfs": func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.ReadOnlyRootFilesystem = ptr.To(false) },
		"privilege escalation": func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext.AllowPrivilegeEscalation = ptr.To(true)
		},
		"privileged":         func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.Privileged = ptr.To(true) },
		"capabilities kept":  func(s *corev1.PodSpec) { s.Containers[0].SecurityContext.Capabilities.Drop = nil },
		"no seccomp profile": func(s *corev1.PodSpec) { s.SecurityContext.SeccompProfile = nil },
		"host network":       func(s *corev1.PodSpec) { s.HostNetwork = true },
		"hostPath volume": func(s *corev1.PodSpec) {
			s.Volumes = []corev1.Volume{{Name: "h", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		},
		"latest tag":           func(s *corev1.PodSpec) { s.Containers[0].Image = "registry.example.com/app:latest" },
		"no digest":            func(s *corev1.PodSpec) { s.Containers[0].Image = "registry.example.com/app:1.0" },
		"registry not allowed": func(s *corev1.PodSpec) { s.Containers[0].Image = "docker.io/library/nginx@" + digest },
		"init container root": func(s *corev1.PodSpec) {
			s.InitContainers = []corev1.Container{{Name: "init", Image: "registry.example.com/init@" + digest}}
		},
		"several violations at once": func(s *corev1.PodSpec) {
			s.HostNetwork = true
			s.Containers[0].SecurityContext.Privileged = ptr.To(true)
			s.Containers[0].Image = "nginx:latest"
		},
	}

	violationsOf := func(message string) []string {
		_, after, found := strings.Cut(message, "violates SecurityBaseline: ")
		if !found {
			return nil
		}
		items := strings.Split(after, "; ")
		slices.Sort(items)
		return items
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := compliant()
			mutate(&spec)

			podValidator := newValidator(t, record.NewFakeRecorder(50), strict)
			podResp := podValidator.Handle(context.Background(), requestWith(t,
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Labels: map[string]string{"app": "web"}}, Spec: spec},
				reqOpts{operation: admissionv1.Create}))

			early := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(50), strict)
			earlyResp := early.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment",
				deploymentOf("web", spec), workloadRequestOpts{}))

			if podResp.Allowed != earlyResp.Allowed {
				t.Fatalf("verdicts differ: Pod admission allowed=%v, early validation allowed=%v", podResp.Allowed, earlyResp.Allowed)
			}
			if podResp.Allowed {
				return
			}
			podViolations, earlyViolations := violationsOf(podResp.Result.Message), violationsOf(earlyResp.Result.Message)
			if !slices.Equal(podViolations, earlyViolations) {
				t.Fatalf("violations differ\nPod admission: %v\nearly:         %v", podViolations, earlyViolations)
			}
			if len(podViolations) == 0 {
				t.Fatal("a denial without violations means the comparison is not exercising anything")
			}
		})
	}
}

func daemonSetOf(name string, spec corev1.PodSpec) *appsv1.DaemonSet {
	labels := map[string]string{"app": name}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: spec},
		},
	}
}

func jobOf(name string, spec corev1.PodSpec) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       batchv1.JobSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: spec}},
	}
}

func cronJobOf(name string, spec corev1.PodSpec) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}}, Spec: spec},
			}},
		},
	}
}

// Every kind that creates Pods gets the same early feedback as a Deployment.
func TestWorkloadValidatorCoversDaemonSetsJobsAndCronJobs(t *testing.T) {
	t.Parallel()
	violating := violatingPod().Spec
	compliant := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true)},
		Containers:      []corev1.Container{{Name: "app", Image: "nginx:1"}},
	}
	builders := map[string]func(string, corev1.PodSpec) any{
		"DaemonSet": func(n string, s corev1.PodSpec) any { return daemonSetOf(n, s) },
		"Job":       func(n string, s corev1.PodSpec) any { return jobOf(n, s) },
		"CronJob":   func(n string, s corev1.PodSpec) any { return cronJobOf(n, s) },
	}
	for kind, build := range builders {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10), rootBaseline())
			ctx := context.Background()

			denied := v.Handle(ctx, workloadRequest(t, admissionv1.Create, kind, build("w", violating), workloadRequestOpts{}))
			if denied.Allowed || !strings.Contains(denied.Result.Message, kind+" team-a/w") {
				t.Fatalf("a violating %s must be rejected naming it, got %+v", kind, denied)
			}
			admitted := v.Handle(ctx, workloadRequest(t, admissionv1.Create, kind, build("w", compliant), workloadRequestOpts{}))
			if !admitted.Allowed || len(admitted.Warnings) != 0 {
				t.Fatalf("a compliant %s must be admitted quietly, got %+v", kind, admitted)
			}
		})
	}
}

func TestWorkloadValidatorOnlyEvaluatesCronJobsWhenTheirJobTemplateChanges(t *testing.T) {
	t.Parallel()
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10), rootBaseline())
	ctx := context.Background()
	legacy := cronJobOf("nightly", violatingPod().Spec)

	suspended := legacy.DeepCopy()
	suspended.Spec.Suspend = ptr.To(true)
	suspended.Spec.Schedule = "30 2 * * *"
	resp := v.Handle(ctx, workloadRequest(t, admissionv1.Update, "CronJob", suspended, workloadRequestOpts{old: legacy}))
	if !resp.Allowed {
		t.Fatalf("changing the schedule or suspending must stay possible on a violating CronJob, got %+v", resp)
	}

	changed := legacy.DeepCopy()
	changed.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = "nginx:2"
	resp = v.Handle(ctx, workloadRequest(t, admissionv1.Update, "CronJob", changed, workloadRequestOpts{old: legacy}))
	if resp.Allowed {
		t.Fatal("a change inside the job template must be evaluated")
	}
}

func TestWorkloadValidatorMatchesTargetingForDaemonSetsAndJobs(t *testing.T) {
	t.Parallel()
	targeted := func(group, kind, name string) *platformv1alpha1.SecurityBaseline {
		b := rootBaseline()
		b.Name = strings.ToLower(kind) + "-" + name
		b.Spec.TargetRef = &platformv1alpha1.PolicyTargetReference{Group: group, Kind: kind, Name: name}
		return b
	}
	v := newWorkloadValidator(t, EarlyValidationEnforce, record.NewFakeRecorder(10),
		targeted("apps", "DaemonSet", "agent"), targeted("batch", "Job", "migrate"))
	ctx := context.Background()
	spec := violatingPod().Spec

	for _, c := range []struct {
		name     string
		kind     string
		obj      any
		wantDeny bool
	}{
		{"targeted DaemonSet", "DaemonSet", daemonSetOf("agent", spec), true},
		{"another DaemonSet", "DaemonSet", daemonSetOf("other", spec), false},
		{"targeted Job", "Job", jobOf("migrate", spec), true},
		{"another Job", "Job", jobOf("cleanup", spec), false},
	} {
		resp := v.Handle(ctx, workloadRequest(t, admissionv1.Create, c.kind, c.obj, workloadRequestOpts{}))
		if resp.Allowed == c.wantDeny {
			t.Errorf("%s: allowed = %v, want denied = %v", c.name, resp.Allowed, c.wantDeny)
		}
	}
}

// podAdmission runs a Pod through the real mutating webhook, applies its JSON patch, and
// validates the result: what the API server does with a Pod.
func podAdmission(t *testing.T, pod *corev1.Pod, objs ...client.Object) admission.Response {
	t.Helper()
	scheme := newWebhookTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	decoder := admission.NewDecoder(scheme)
	mutator := &PodMutator{Client: c, Recorder: record.NewFakeRecorder(50), decoder: decoder}
	validator := &PodValidator{Client: c, Recorder: record.NewFakeRecorder(50), decoder: decoder}

	mutation := mutator.Handle(context.Background(), requestWith(t, pod, reqOpts{operation: admissionv1.Create}))
	if !mutation.Allowed {
		t.Fatalf("the mutating webhook rejected the Pod: %+v", mutation)
	}
	original, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	mutatedRaw := original
	if len(mutation.Patches) > 0 {
		operations, err := json.Marshal(mutation.Patches)
		if err != nil {
			t.Fatal(err)
		}
		patch, err := jsonpatch.DecodePatch(operations)
		if err != nil {
			t.Fatal(err)
		}
		if mutatedRaw, err = patch.Apply(original); err != nil {
			t.Fatal(err)
		}
	}
	mutated := &corev1.Pod{}
	if err := json.Unmarshal(mutatedRaw, mutated); err != nil {
		t.Fatal(err)
	}
	return validator.Handle(context.Background(), requestWith(t, mutated, reqOpts{operation: admissionv1.Create}))
}

// G1: a baseline that selects on a label that only this operator's mutator adds. Pod
// admission sees the label (the mutator runs first); the early check must too.
func TestEarlyValidationSeesTheLabelsThePodMutatorAdds(t *testing.T) {
	t.Parallel()
	labeller := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "team-label", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]platformv1alpha1.LabelValueString{"team": "platform"},
		},
	}
	teamBaseline := rootBaseline()
	teamBaseline.Name = "platform-team"
	teamBaseline.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"team": "platform"}}

	for name, objs := range map[string][]client.Object{
		"with the policy that adds the label":    {labeller, teamBaseline},
		"without it the baseline does not apply": {teamBaseline},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := violatingPod().Spec
			podResp := podAdmission(t, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-0", Labels: map[string]string{"app": "web"}}, Spec: spec}, objs...)

			scheme := newWebhookTestScheme(t)
			early := &WorkloadValidator{
				Client:   fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
				Recorder: record.NewFakeRecorder(50), Mode: EarlyValidationEnforce, decoder: admission.NewDecoder(scheme),
			}
			earlyResp := early.Handle(context.Background(),
				workloadRequest(t, admissionv1.Create, "Deployment", deploymentOf("web", spec), workloadRequestOpts{}))

			if podResp.Allowed != earlyResp.Allowed {
				t.Fatalf("verdicts differ: Pod admission allowed=%v, early validation allowed=%v", podResp.Allowed, earlyResp.Allowed)
			}
			if len(objs) == 2 && podResp.Allowed {
				t.Fatal("the label added by the mutator must bring the team baseline in, in both paths")
			}
		})
	}
}

func TestEarlyValidationDoesNotMutateTheWorkloadItReceived(t *testing.T) {
	t.Parallel()
	labeller := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "team-label", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]platformv1alpha1.LabelValueString{"team": "platform"},
			DefaultRequests: map[string]platformv1alpha1.QuantityString{"cpu": "100m"},
		},
	}
	v := newWorkloadValidator(t, EarlyValidationWarn, record.NewFakeRecorder(10), labeller, rootBaseline())
	web := deploymentOf("web", violatingPod().Spec)
	before := web.DeepCopy()

	resp := v.Handle(context.Background(), workloadRequest(t, admissionv1.Create, "Deployment", web, workloadRequestOpts{}))

	if !resp.Allowed || len(resp.Patches) != 0 {
		t.Fatalf("the early check only reports, it never patches; got %+v", resp)
	}
	if !reflect.DeepEqual(before, web) {
		t.Fatal("the request object was modified")
	}
}
