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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// SecurityBaselineReconciler reconciles a SecurityBaseline object
type SecurityBaselineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=securitybaselines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=securitybaselines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=securitybaselines/finalizers,verbs=update

// Reconcile reconciles a SecurityBaseline object by updating its status condition
// to Available once the resource is observed. The actual enforcement is delegated
// to the Pod validating webhook (PodValidator).
func (r *SecurityBaselineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var baseline corev1alpha1.SecurityBaseline
	if err := r.Get(ctx, req.NamespacedName, &baseline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling SecurityBaseline", "name", baseline.Name, "namespace", baseline.Namespace)

	// Update status to Available
	meta.SetStatusCondition(&baseline.Status.Conditions, metav1.Condition{
		Type:    "Available",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "SecurityBaseline is available and being enforced",
	})

	if err := r.Status().Update(ctx, &baseline); err != nil {
		log.Error(err, "Failed to update SecurityBaseline status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(&baseline, "Normal", "Reconciled", "SecurityBaseline is available and being enforced")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecurityBaselineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.SecurityBaseline{}).
		Named("securitybaseline").
		Complete(r)
}
