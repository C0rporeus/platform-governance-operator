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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// WorkloadPolicyReconciler reconciles a WorkloadPolicy object
type WorkloadPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=workloadpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=workloadpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=workloadpolicies/finalizers,verbs=update

// Reconcile reconciles a WorkloadPolicy object by updating its status condition
// to Available once the resource is observed. The actual policy enforcement
// (default labels, resource requests/limits) is delegated to the Pod mutating
// webhook (PodMutator).
func (r *WorkloadPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var policy corev1alpha1.WorkloadPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling WorkloadPolicy", "name", policy.Name, "namespace", policy.Namespace)

	updated, err := updateAvailableStatusIfChanged(
		ctx,
		r.Status(),
		r.Recorder,
		&policy,
		&policy.Status.Conditions,
		"WorkloadPolicy is available and being enforced",
	)
	if err != nil {
		log.Error(err, "Failed to update WorkloadPolicy status")
		return ctrl.Result{}, err
	}
	if !updated {
		log.V(1).Info("Skipping status update; WorkloadPolicy already marked Available", "name", policy.Name, "namespace", policy.Namespace)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkloadPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.WorkloadPolicy{}).
		Named("workloadpolicy").
		Complete(r)
}
