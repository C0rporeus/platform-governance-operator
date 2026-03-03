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

package controller

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newStatusHelperScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add corev1alpha1 to scheme: %v", err)
	}
	return s
}

// mockFailingStatusWriter is a client.StatusWriter whose Update always errors.
type mockFailingStatusWriter struct{}

func (m *mockFailingStatusWriter) Create(_ context.Context, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
	return nil
}

func (m *mockFailingStatusWriter) Update(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
	return fmt.Errorf("simulated status update error")
}

func (m *mockFailingStatusWriter) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
	return nil
}

func (m *mockFailingStatusWriter) Apply(_ context.Context, _ runtime.ApplyConfiguration, _ ...client.SubResourceApplyOption) error {
	return nil
}

// ─── tests ────────────────────────────────────────────────────────────────────

func TestUpdateAvailableStatusIfChangedFirstCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newStatusHelperScheme(t)
	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-avail", Namespace: "default"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(policy).
		Build()

	// Create via fake client so ResourceVersion is populated (required by Status().Update())
	if err := cl.Create(ctx, policy); err != nil {
		t.Fatalf("create: %v", err)
	}

	recorder := record.NewFakeRecorder(10)
	changed, err := updateAvailableStatusIfChanged(ctx, cl.Status(), recorder, policy, &policy.Status.Conditions, "first call message")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on first call")
	}

	select {
	case event := <-recorder.Events:
		if event == "" {
			t.Fatalf("expected a non-empty event string")
		}
	default:
		t.Fatalf("expected an event to be recorded on first call")
	}
}

func TestUpdateAvailableStatusIfChangedIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newStatusHelperScheme(t)
	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-idempotent", Namespace: "default"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(policy).
		Build()

	if err := cl.Create(ctx, policy); err != nil {
		t.Fatalf("create: %v", err)
	}

	recorder := record.NewFakeRecorder(10)
	_, _ = updateAvailableStatusIfChanged(ctx, cl.Status(), recorder, policy, &policy.Status.Conditions, "msg")

	// Fetch the updated object so its ResourceVersion is current before the second call
	updated := &corev1alpha1.WorkloadPolicy{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "test-idempotent", Namespace: "default"}, updated); err != nil {
		t.Fatalf("get after first call: %v", err)
	}

	recorder2 := record.NewFakeRecorder(10)
	changed, err := updateAvailableStatusIfChanged(ctx, cl.Status(), recorder2, updated, &updated.Status.Conditions, "msg")

	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false on second call (idempotent)")
	}

	select {
	case event := <-recorder2.Events:
		t.Fatalf("expected no event on idempotent second call, got: %s", event)
	default:
		// correct: no event when condition is already set
	}
}

func TestUpdateAvailableStatusIfChangedNilRecorder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newStatusHelperScheme(t)
	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-nil-recorder", Namespace: "default"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(policy).
		Build()

	if err := cl.Create(ctx, policy); err != nil {
		t.Fatalf("create: %v", err)
	}

	// nil recorder must not panic
	changed, err := updateAvailableStatusIfChanged(ctx, cl.Status(), nil, policy, &policy.Status.Conditions, "no recorder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on first call with nil recorder")
	}
}

func TestUpdateAvailableStatusIfChangedStatusWriterError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	policy := &corev1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-err", Namespace: "default"},
	}
	recorder := record.NewFakeRecorder(10)

	changed, err := updateAvailableStatusIfChanged(ctx, &mockFailingStatusWriter{}, recorder, policy, &policy.Status.Conditions, "msg")

	if err == nil {
		t.Fatalf("expected an error from the failing status writer")
	}
	if changed {
		t.Fatalf("expected changed=false when status update fails")
	}

	select {
	case event := <-recorder.Events:
		t.Fatalf("expected no event when status update fails, got: %s", event)
	default:
		// correct: no event when update failed
	}
}
