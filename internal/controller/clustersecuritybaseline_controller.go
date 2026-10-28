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

// ClusterSecurityBaselineReconciler reconciles a ClusterSecurityBaseline object
type ClusterSecurityBaselineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=clustersecuritybaselines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=clustersecuritybaselines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.platform.f3nr1r.io,resources=clustersecuritybaselines/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile marks a ClusterSecurityBaseline Available once observed. Enforcement
// is delegated to the Pod validating webhook (PodValidator).
func (r *ClusterSecurityBaselineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var baseline corev1alpha1.ClusterSecurityBaseline
	if err := r.Get(ctx, req.NamespacedName, &baseline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling ClusterSecurityBaseline", "name", baseline.Name)

	updated, err := updateAvailableStatusIfChanged(
		ctx,
		r.Status(),
		r.Recorder,
		&baseline,
		&baseline.Status.Conditions,
		"ClusterSecurityBaseline is available and being enforced",
	)
	if err != nil {
		log.Error(err, "Failed to update ClusterSecurityBaseline status")
		return ctrl.Result{}, err
	}
	if !updated {
		log.V(1).Info("Skipping status update; ClusterSecurityBaseline already marked Available", "name", baseline.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterSecurityBaselineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.ClusterSecurityBaseline{}).
		Named("clustersecuritybaseline").
		Complete(r)
}
