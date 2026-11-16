package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	platform "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func TestAuditConditionStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report AuditReport
		err    error
		status metav1.ConditionStatus
		reason string
	}{
		{"passed", AuditReport{Complete: true, Evaluated: 2}, nil, metav1.ConditionTrue, "AssessmentPassed"},
		{"violation", AuditReport{Complete: true, Evaluated: 2, Violating: 1}, nil, metav1.ConditionFalse, "ViolationsFound"},
		{"partial clean", AuditReport{Evaluated: 2}, nil, metav1.ConditionUnknown, "AssessmentIncomplete"},
		{"partial violating", AuditReport{Evaluated: 2, Violating: 1}, nil, metav1.ConditionUnknown, "AssessmentIncomplete"},
		{"failed", AuditReport{}, errors.New("read failed"), metav1.ConditionUnknown, "AssessmentFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := auditCondition(7, tc.report, tc.err)
			if got.Status != tc.status || got.Reason != tc.reason || got.ObservedGeneration != 7 {
				t.Fatalf("unexpected condition: %+v", got)
			}
		})
	}
}

func TestPersistAuditIsIdempotentAndOnlyEmitsTransitions(t *testing.T) {
	ctx := context.Background()
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: "team", Generation: 1}}
	c := fake.NewClientBuilder().WithScheme(newStatusHelperScheme(t)).WithStatusSubresource(baseline).WithObjects(baseline).Build()
	recorder := record.NewFakeRecorder(10)
	report := AuditReport{Complete: true, Evaluated: 2}
	if err := persistAudit(ctx, c, recorder, baseline, report, nil); err != nil {
		t.Fatal(err)
	}
	expectAuditEvent(t, recorder)
	version := baseline.ResourceVersion
	if err := persistAudit(ctx, c, recorder, baseline, report, nil); err != nil {
		t.Fatal(err)
	}
	if version != baseline.ResourceVersion {
		t.Fatal("unchanged assessment rewrote status")
	}
	report.Violating = 1
	if err := persistAudit(ctx, c, recorder, baseline, report, nil); err != nil {
		t.Fatal(err)
	}
	expectAuditEvent(t, recorder)
	report.Violating = 2
	if err := persistAudit(ctx, c, recorder, baseline, report, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-recorder.Events:
		t.Fatalf("count change emitted transition: %s", event)
	default:
	}
	failure := errors.New("cache failed")
	if err := persistAudit(ctx, c, recorder, baseline, AuditReport{}, failure); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	stored := &platform.SecurityBaseline{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(baseline), stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Compliance.Complete || meta.FindStatusCondition(stored.Status.Conditions, "Compliant").Status != metav1.ConditionUnknown {
		t.Fatalf("failed assessment retained compliance: %+v", stored.Status)
	}
}

func expectAuditEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()
	select {
	case <-recorder.Events:
	case <-time.After(time.Second):
		t.Fatal("missing compliance transition event")
	}
}

func TestPersistAuditStatusFailureDoesNotEmitEvent(t *testing.T) {
	baseline := &platform.SecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: "team"}}
	failed := errors.New("status write failed")
	c := fake.NewClientBuilder().WithScheme(newStatusHelperScheme(t)).WithStatusSubresource(baseline).
		WithObjects(baseline).WithInterceptorFuncs(interceptor.Funcs{
		SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
			return failed
		},
	}).Build()
	recorder := record.NewFakeRecorder(10)
	err := persistAudit(context.Background(), c, recorder, baseline, AuditReport{Complete: true}, nil)
	if !errors.Is(err, failed) {
		t.Fatalf("status error lost: %v", err)
	}
	select {
	case event := <-recorder.Events:
		t.Fatalf("unpersisted state emitted event: %s", event)
	default:
	}
}

func TestPersistClusterAuditIncludesExamplesAndGeneration(t *testing.T) {
	ctx := context.Background()
	baseline := &platform.ClusterSecurityBaseline{ObjectMeta: metav1.ObjectMeta{Name: "global", Generation: 4}}
	c := fake.NewClientBuilder().WithScheme(newStatusHelperScheme(t)).WithStatusSubresource(baseline).WithObjects(baseline).Build()
	report := AuditReport{Complete: true, Evaluated: 1, Violating: 1,
		Examples: []platform.ComplianceExample{{Namespace: "team", Pod: "workload", Rule: "RunAsNonRoot"}}}
	if err := persistAudit(ctx, c, nil, baseline, report, nil); err != nil {
		t.Fatal(err)
	}
	stored := &platform.ClusterSecurityBaseline{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(baseline), stored); err != nil {
		t.Fatal(err)
	}
	summary := stored.Status.Compliance
	if summary.ObservedGeneration != 4 || len(summary.Examples) != 1 || summary.Violating != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
