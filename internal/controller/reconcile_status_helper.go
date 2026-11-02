package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Controllers and the admission webhooks record events through the manager's recorder,
// including in workload namespaces, so the manager needs permission to write them.
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// updateAvailableStatusIfChanged sets the Available condition and records an event when it changes.
func updateAvailableStatusIfChanged(
	ctx context.Context,
	statusWriter client.StatusWriter,
	recorder record.EventRecorder,
	obj client.Object,
	conditions *[]metav1.Condition,
	message string,
) (bool, error) {
	changed := meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            message,
		ObservedGeneration: obj.GetGeneration(),
	})
	if !changed {
		return false, nil
	}

	if err := statusWriter.Update(ctx, obj); err != nil {
		return false, err
	}

	if recorder != nil {
		recorder.Event(obj, "Normal", "Reconciled", message)
	}

	return true, nil
}
