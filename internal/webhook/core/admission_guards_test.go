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
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	platformv1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

type reqOpts struct {
	operation admissionv1.Operation
	oldPod    *corev1.Pod
	user      authenticationv1.UserInfo
	dryRun    bool
}

func requestWith(t *testing.T, pod *corev1.Pod, o reqOpts) admission.Request {
	t.Helper()
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	r := admissionv1.AdmissionRequest{
		Namespace: testNamespace,
		Operation: o.operation,
		Object:    runtime.RawExtension{Raw: raw},
		UserInfo:  o.user,
	}
	if o.oldPod != nil {
		oldRaw, err := json.Marshal(o.oldPod)
		if err != nil {
			t.Fatal(err)
		}
		r.OldObject = runtime.RawExtension{Raw: oldRaw}
	}
	if o.dryRun {
		yes := true
		r.DryRun = &yes
	}
	return admission.Request{AdmissionRequest: r}
}

func rootBaseline() *platformv1alpha1.SecurityBaseline {
	return &platformv1alpha1.SecurityBaseline{
		ObjectMeta: metav1.ObjectMeta{Name: "strict", Namespace: testNamespace},
		Spec:       platformv1alpha1.SecurityBaselineSpec{RunAsNonRoot: true},
	}
}

func newValidator(t *testing.T, recorder record.EventRecorder, objs ...*platformv1alpha1.SecurityBaseline) *PodValidator {
	t.Helper()
	scheme := newWebhookTestScheme(t)
	b := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	return &PodValidator{Client: b.Build(), Recorder: recorder, decoder: admission.NewDecoder(scheme)}
}

func violatingPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy", Labels: map[string]string{"app": "legacy"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1"}}},
	}
}

// BUG-01.3: Pod Security admission exempts updates that only change metadata,
// activeDeadlineSeconds and tolerations; a running Pod that violates a newer
// baseline must still be relabellable.
func TestPodValidatorExemptsMetadataOnlyUpdates(t *testing.T) {
	t.Parallel()
	v := newValidator(t, record.NewFakeRecorder(10), rootBaseline())

	oldPod := violatingPod()
	newPod := violatingPod()
	newPod.Labels["owner"] = "team-a"
	newPod.Annotations = map[string]string{"note": "patched by a controller"}
	deadline := int64(60)
	newPod.Spec.ActiveDeadlineSeconds = &deadline
	newPod.Spec.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}

	resp := v.Handle(context.Background(), requestWith(t, newPod, reqOpts{operation: admissionv1.Update, oldPod: oldPod}))
	if !resp.Allowed {
		t.Fatalf("metadata-only update of a violating Pod must be allowed, got %v", resp.Result)
	}
}

func TestPodValidatorStillEvaluatesRelevantUpdates(t *testing.T) {
	t.Parallel()

	changes := map[string]func(old, updated *corev1.Pod){
		"image change": func(_, u *corev1.Pod) { u.Spec.Containers[0].Image = "nginx:2" },
		"seccomp annotation change": func(_, u *corev1.Pod) {
			u.Annotations = map[string]string{"container.seccomp.security.alpha.kubernetes.io/app": "unconfined"}
		},
	}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := newValidator(t, record.NewFakeRecorder(10), rootBaseline())
			oldPod, updated := violatingPod(), violatingPod()
			mutate(oldPod, updated)
			resp := v.Handle(context.Background(), requestWith(t, updated, reqOpts{operation: admissionv1.Update, oldPod: oldPod}))
			if resp.Allowed {
				t.Fatalf("%s must still be evaluated and denied", name)
			}
		})
	}
}

func TestPodValidatorCreateIsAlwaysEvaluated(t *testing.T) {
	t.Parallel()
	v := newValidator(t, record.NewFakeRecorder(10), rootBaseline())
	resp := v.Handle(context.Background(), requestWith(t, violatingPod(), reqOpts{operation: admissionv1.Create}))
	if resp.Allowed {
		t.Fatal("creating a violating Pod must be denied")
	}
}

// Mutating a running Pod would be rejected by the API server (spec is
// immutable) and block benign updates, so the mutator acts on create only.
func TestPodMutatorIgnoresUpdates(t *testing.T) {
	t.Parallel()
	scheme := newWebhookTestScheme(t)
	policy := &platformv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "defaults", Namespace: testNamespace},
		Spec: platformv1alpha1.WorkloadPolicySpec{
			MandatoryLabels: map[string]string{"env": "prod"},
			DefaultRequests: map[string]string{"cpu": "100m"},
		},
	}
	m := &PodMutator{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).Build(),
		Recorder: record.NewFakeRecorder(10), decoder: admission.NewDecoder(scheme)}

	resp := m.Handle(context.Background(), requestWith(t, violatingPod(), reqOpts{operation: admissionv1.Update, oldPod: violatingPod()}))
	if !resp.Allowed || len(resp.Patches) != 0 {
		t.Fatalf("update must pass untouched, got allowed=%v patches=%v", resp.Allowed, resp.Patches)
	}
	resp = m.Handle(context.Background(), requestWith(t, violatingPod(), reqOpts{operation: admissionv1.Create}))
	if len(resp.Patches) == 0 {
		t.Fatal("create must still be mutated")
	}
}

// BUG-01.5: static Pod mirrors are written by the kubelet. The annotation alone
// is user-settable, so it must never exempt a request by itself.
func TestMirrorPodsAreSkippedOnlyForNodeIdentities(t *testing.T) {
	t.Parallel()
	mirror := func() *corev1.Pod {
		p := violatingPod()
		p.Annotations = map[string]string{mirrorPodAnnotation: "abc123"}
		return p
	}
	node := authenticationv1.UserInfo{Username: "system:node:worker-1", Groups: []string{"system:nodes", "system:authenticated"}}
	user := authenticationv1.UserInfo{Username: "alice", Groups: []string{"developers"}}
	fakeNodeName := authenticationv1.UserInfo{Username: "system:node:worker-1", Groups: []string{"developers"}}
	fakeNodesGroup := authenticationv1.UserInfo{Username: "alice", Groups: []string{"system:nodes"}}

	v := newValidator(t, record.NewFakeRecorder(10), rootBaseline())
	if resp := v.Handle(context.Background(), requestWith(t, mirror(), reqOpts{operation: admissionv1.Create, user: node})); !resp.Allowed {
		t.Fatalf("mirror Pod written by a node must be skipped: %v", resp.Result)
	}
	for name, u := range map[string]authenticationv1.UserInfo{"ordinary user": user, "node-like name without the nodes group": fakeNodeName, "nodes group without a node username": fakeNodesGroup} {
		if resp := v.Handle(context.Background(), requestWith(t, mirror(), reqOpts{operation: admissionv1.Create, user: u})); resp.Allowed {
			t.Fatalf("%s setting the mirror annotation must not bypass the baseline", name)
		}
	}
}

// BUG-01.4: Windows Pods are exempt from the Linux-only controls.
func TestWindowsPodsSkipLinuxOnlyRules(t *testing.T) {
	t.Parallel()
	yes := true
	baseline := &platformv1alpha1.SecurityBaseline{Spec: platformv1alpha1.SecurityBaselineSpec{
		ReadOnlyRootFilesystem:      true,
		DisallowPrivilegeEscalation: &yes,
		RequireDropAllCapabilities:  &yes,
		RequireSeccompProfile:       &yes,
	}}
	pod := violatingPod()
	linux := EvaluateSecurityBaseline(pod, baseline)
	if len(linux) == 0 {
		t.Fatal("precondition: the Linux Pod must violate the baseline")
	}
	pod.Spec.OS = &corev1.PodOS{Name: corev1.Windows}
	if got := EvaluateSecurityBaseline(pod, baseline); len(got) != 0 {
		t.Fatalf("Windows Pod must skip Linux-only rules, got %v", got)
	}
	// Non-root still applies to Windows Pods.
	rootOnly := &platformv1alpha1.SecurityBaseline{Spec: platformv1alpha1.SecurityBaselineSpec{RunAsNonRoot: true}}
	if got := EvaluateSecurityBaseline(pod, rootOnly); len(got) == 0 {
		t.Fatal("runAsNonRoot applies to Windows Pods too")
	}
}

// BUG-01.2 (dry-run part): sideEffects None means no events for dry runs.
func TestNoEventsOnDryRun(t *testing.T) {
	t.Parallel()
	rec := record.NewFakeRecorder(10)
	v := newValidator(t, rec, rootBaseline())
	v.Handle(context.Background(), requestWith(t, violatingPod(), reqOpts{operation: admissionv1.Create, dryRun: true}))
	if len(rec.Events) != 0 {
		t.Fatalf("dry run must not record events, got %d", len(rec.Events))
	}
	v.Handle(context.Background(), requestWith(t, violatingPod(), reqOpts{operation: admissionv1.Create}))
	if len(rec.Events) == 0 {
		t.Fatal("a real request must record an event")
	}
}

// BUG-01.1: the ISO references printed in violations must match control titles.
func TestISOControlReferences(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"RunAsNonRoot":                "ISO/IEC 27001 A.8.2",
		"ReadOnlyRootFilesystem":      "ISO/IEC 27001 A.8.9",
		"DisallowHostNamespaces":      "ISO/IEC 27001 A.8.9",
		"DisallowHostPath":            "ISO/IEC 27001 A.8.9",
		"SupplyChain":                 "ISO/IEC 27001 A.5.21",
		"DisallowPrivileged":          "ISO/IEC 27001 A.8.2",
		"DropAllCapabilities":         "ISO/IEC 27001 A.8.2",
		"SeccompProfile":              "ISO/IEC 27001 A.8.2",
		"DisallowPrivilegeEscalation": "ISO/IEC 27001 A.8.2",
	}
	for _, rule := range DefaultRules() {
		expected, ok := want[rule.Name()]
		if !ok {
			t.Fatalf("rule %q has no expected control; update the mapping test", rule.Name())
		}
		if rule.ISOControl() != expected {
			t.Errorf("%s: got %q, want %q", rule.Name(), rule.ISOControl(), expected)
		}
	}
}
