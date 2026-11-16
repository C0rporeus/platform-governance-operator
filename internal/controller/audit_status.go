package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platform "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

func persistAudit(ctx context.Context, c client.Client, recorder record.EventRecorder, obj client.Object,
	report AuditReport, scanErr error) error {
	var summary **platform.ComplianceSummary
	var conditions *[]metav1.Condition
	switch baseline := obj.(type) {
	case *platform.SecurityBaseline:
		summary, conditions = &baseline.Status.Compliance, &baseline.Status.Conditions
	case *platform.ClusterSecurityBaseline:
		summary, conditions = &baseline.Status.Compliance, &baseline.Status.Conditions
	default:
		return fmt.Errorf("unsupported audit status object %T", obj)
	}
	previous := meta.FindStatusCondition(*conditions, "Compliant")
	condition := auditCondition(obj.GetGeneration(), report, scanErr)
	transition := previous == nil || previous.Status != condition.Status
	before := obj.DeepCopyObject()
	*summary = &platform.ComplianceSummary{ObservedGeneration: obj.GetGeneration(), Inspected: report.Inspected,
		Evaluated: report.Evaluated, Violating: report.Violating, Complete: report.Complete && scanErr == nil,
		Examples: report.Examples}
	meta.SetStatusCondition(conditions, condition)
	if equality.Semantic.DeepEqual(before, obj) {
		return scanErr
	}
	if err := c.Status().Update(ctx, obj); err != nil {
		return errors.Join(scanErr, err)
	}
	if transition && recorder != nil {
		eventType := corev1.EventTypeNormal
		if condition.Status != metav1.ConditionTrue {
			eventType = corev1.EventTypeWarning
		}
		recorder.Event(obj, eventType, condition.Reason, condition.Message)
	}
	return scanErr
}

func auditCondition(generation int64, report AuditReport, scanErr error) metav1.Condition {
	condition := metav1.Condition{Type: "Compliant", ObservedGeneration: generation,
		Status: metav1.ConditionUnknown, Reason: "AssessmentIncomplete", Message: "Pod assessment is incomplete"}
	switch {
	case scanErr != nil:
		condition.Reason, condition.Message = "AssessmentFailed", "Pod assessment failed; retry pending"
	case !report.Complete:
		condition.Message = fmt.Sprintf("Inspection budget reached after %d Pods; full compliance is unknown", report.Inspected)
	case report.Violating > 0:
		condition.Status, condition.Reason = metav1.ConditionFalse, "ViolationsFound"
		condition.Message = fmt.Sprintf("%d of %d evaluated Pods violate this baseline", report.Violating, report.Evaluated)
	default:
		condition.Status, condition.Reason = metav1.ConditionTrue, "AssessmentPassed"
		condition.Message = fmt.Sprintf("All %d evaluated Pods satisfy this baseline", report.Evaluated)
	}
	return condition
}
