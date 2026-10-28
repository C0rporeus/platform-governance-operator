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
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	policyresolver "github.com/f3nr1r/platform-governance-operator/internal/policy"
)

const (
	deploymentHPAEnabledAnnotation        = "core.platform.f3nr1r.io/hpa-enabled"
	managedHPALabelKey                    = "core.platform.f3nr1r.io/managed-hpa"
	managedHPALabelValue                  = "true"
	managedHPAWorkloadPolicyAnnotationKey = "core.platform.f3nr1r.io/workload-policy"
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
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete

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

	if policy.Spec.HorizontalScaling != nil {
		if err := r.reconcileDeploymentHPAs(ctx, &policy); err != nil {
			log.Error(err, "Failed to reconcile horizontal scaling for Deployments")
			return ctrl.Result{}, err
		}
	}

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
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var policies corev1alpha1.WorkloadPolicyList
				if err := r.List(ctx, &policies, client.InNamespace(obj.GetNamespace())); err != nil {
					return nil
				}

				requests := make([]reconcile.Request, 0, len(policies.Items))
				for _, policy := range policies.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      policy.Name,
							Namespace: policy.Namespace,
						},
					})
				}
				return requests
			}),
		).
		Named("workloadpolicy").
		Complete(r)
}

// horizontalScalingPolicies lists the policies in the namespace that define horizontal scaling.
func (r *WorkloadPolicyReconciler) horizontalScalingPolicies(ctx context.Context, namespace string) ([]corev1alpha1.WorkloadPolicy, error) {
	var policies corev1alpha1.WorkloadPolicyList
	if err := r.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	withScaling := make([]corev1alpha1.WorkloadPolicy, 0, len(policies.Items))
	for i := range policies.Items {
		if policies.Items[i].Spec.HorizontalScaling != nil {
			withScaling = append(withScaling, policies.Items[i])
		}
	}
	return withScaling, nil
}

// governingPolicyName returns the name of the most specific (then highest
// priority) horizontal scaling policy for the deployment, or "" if none applies.
func governingPolicyName(
	ctx context.Context,
	candidates []corev1alpha1.WorkloadPolicy,
	deployment *appsv1.Deployment,
) string {
	ordered, errs := policyresolver.Select(candidates, policyresolver.SubjectForDeployment(deployment), workloadPolicyCandidate)
	for _, err := range errs {
		logf.FromContext(ctx).Error(err, "Skipping WorkloadPolicy with invalid selector", "deployment", deployment.Name)
	}
	if len(ordered) == 0 {
		return ""
	}
	return ordered[0].Name
}

// releaseUngovernedHPA deletes the managed HPA of a Deployment that no policy
// governs any more, but only if this policy created it. HPAs owned by another
// policy or not managed by the operator are left untouched.
func (r *WorkloadPolicyReconciler) releaseUngovernedHPA(
	ctx context.Context,
	policy *corev1alpha1.WorkloadPolicy,
	deployment *appsv1.Deployment,
) error {
	existing := &autoscalingv2.HorizontalPodAutoscaler{}
	key := types.NamespacedName{Name: managedHPAName(deployment.Name), Namespace: deployment.Namespace}
	if err := r.Get(ctx, key, existing); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !isManagedHPA(existing) || existing.Annotations[managedHPAWorkloadPolicyAnnotationKey] != policy.Name {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, existing))
}

func workloadPolicyCandidate(p corev1alpha1.WorkloadPolicy) policyresolver.Candidate {
	return policyresolver.Candidate{
		Name:      p.Name,
		Namespace: p.Namespace,
		Priority:  p.Spec.Priority,
		Targeting: p.Spec.PolicyTargeting,
	}
}

func (r *WorkloadPolicyReconciler) reconcileDeploymentHPAs(ctx context.Context, policy *corev1alpha1.WorkloadPolicy) error {
	log := logf.FromContext(ctx)

	candidates, err := r.horizontalScalingPolicies(ctx, policy.Namespace)
	if err != nil {
		return err
	}

	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments, client.InNamespace(policy.Namespace)); err != nil {
		return err
	}

	for i := range deployments.Items {
		deployment := &deployments.Items[i]

		switch governing := governingPolicyName(ctx, candidates, deployment); governing {
		case policy.Name:
			// This policy governs the Deployment: reconcile its HPA below.
		case "":
			if err := r.releaseUngovernedHPA(ctx, policy, deployment); err != nil {
				return err
			}
			continue
		default:
			log.V(1).Info("Skipping Deployment governed by another WorkloadPolicy",
				"policy", policy.Name, "governing", governing, "deployment", deployment.Name, "namespace", policy.Namespace)
			continue
		}

		enabled, err := hpaEnabledForDeployment(deployment, policy.Spec.HorizontalScaling)
		if err != nil {
			return err
		}

		desiredHPAName := managedHPAName(deployment.Name)
		key := types.NamespacedName{
			Name:      desiredHPAName,
			Namespace: deployment.Namespace,
		}

		existingHPA := &autoscalingv2.HorizontalPodAutoscaler{}
		err = r.Get(ctx, key, existingHPA)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		if !enabled {
			if apierrors.IsNotFound(err) {
				continue
			}
			if isManagedHPA(existingHPA) {
				if deleteErr := r.Delete(ctx, existingHPA); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
					return deleteErr
				}
			}
			continue
		}

		desiredHPA := desiredHPAForDeployment(policy, deployment)

		if apierrors.IsNotFound(err) {
			if createErr := r.Create(ctx, desiredHPA); createErr != nil {
				return createErr
			}
			continue
		}

		if !isManagedHPA(existingHPA) {
			return fmt.Errorf(
				"horizontalpodautoscaler %s/%s exists but is not managed by platform-governance-operator",
				existingHPA.Namespace,
				existingHPA.Name,
			)
		}

		if hpaSpecDrifted(existingHPA, desiredHPA) {
			existingHPA.Spec = desiredHPA.Spec
			ensureManagedHPAMetadata(existingHPA, policy.Name)
			if updateErr := r.Update(ctx, existingHPA); updateErr != nil {
				return updateErr
			}
		}
	}

	return nil
}

func desiredHPAForDeployment(policy *corev1alpha1.WorkloadPolicy, deployment *appsv1.Deployment) *autoscalingv2.HorizontalPodAutoscaler {
	hpaPolicy := effectiveHorizontalScalingPolicy(policy.Spec.HorizontalScaling)
	minReplicas := hpaPolicy.MinReplicas
	targetCPU := hpaPolicy.TargetCPUUtilizationPercentage

	blockOwnerDeletion := true
	isController := true

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedHPAName(deployment.Name),
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				managedHPALabelKey: managedHPALabelValue,
			},
			Annotations: map[string]string{
				managedHPAWorkloadPolicyAnnotationKey: policy.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         corev1alpha1.GroupVersion.String(),
					Kind:               "WorkloadPolicy",
					Name:               policy.Name,
					UID:                policy.UID,
					BlockOwnerDeletion: &blockOwnerDeletion,
					Controller:         &isController,
				},
			},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Name:       deployment.Name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: hpaPolicy.MaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
	}

	return hpa
}

func hpaEnabledForDeployment(
	deployment *appsv1.Deployment,
	hpaPolicy *corev1alpha1.HorizontalScalingPolicy,
) (bool, error) {
	effectivePolicy := effectiveHorizontalScalingPolicy(hpaPolicy)
	enabled := effectivePolicy.EnabledByDefault
	if deployment.Annotations == nil {
		return enabled, nil
	}

	raw, exists := deployment.Annotations[deploymentHPAEnabledAnnotation]
	if !exists {
		return enabled, nil
	}

	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf(
			"deployment %s/%s has invalid %s annotation value %q",
			deployment.Namespace,
			deployment.Name,
			deploymentHPAEnabledAnnotation,
			raw,
		)
	}

	return parsed, nil
}

func effectiveHorizontalScalingPolicy(hpaPolicy *corev1alpha1.HorizontalScalingPolicy) corev1alpha1.HorizontalScalingPolicy {
	if hpaPolicy == nil {
		return corev1alpha1.HorizontalScalingPolicy{
			EnabledByDefault:               false,
			MinReplicas:                    corev1alpha1.DefaultHPAMinReplicas,
			MaxReplicas:                    corev1alpha1.DefaultHPAMaxReplicas,
			TargetCPUUtilizationPercentage: corev1alpha1.DefaultHPATargetCPU,
		}
	}

	effective := *hpaPolicy
	if effective.MinReplicas == 0 {
		effective.MinReplicas = corev1alpha1.DefaultHPAMinReplicas
	}
	if effective.MaxReplicas == 0 {
		effective.MaxReplicas = corev1alpha1.DefaultHPAMaxReplicas
	}
	if effective.TargetCPUUtilizationPercentage == 0 {
		effective.TargetCPUUtilizationPercentage = corev1alpha1.DefaultHPATargetCPU
	}

	return effective
}

func managedHPAName(deploymentName string) string {
	const suffix = "-pgo-hpa"
	maxBaseLen := 63 - len(suffix)
	if len(deploymentName) > maxBaseLen {
		deploymentName = deploymentName[:maxBaseLen]
	}
	return deploymentName + suffix
}

func isManagedHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa == nil {
		return false
	}
	if hpa.Labels == nil {
		return false
	}
	return hpa.Labels[managedHPALabelKey] == managedHPALabelValue
}

func ensureManagedHPAMetadata(hpa *autoscalingv2.HorizontalPodAutoscaler, policyName string) {
	if hpa.Labels == nil {
		hpa.Labels = map[string]string{}
	}
	hpa.Labels[managedHPALabelKey] = managedHPALabelValue

	if hpa.Annotations == nil {
		hpa.Annotations = map[string]string{}
	}
	hpa.Annotations[managedHPAWorkloadPolicyAnnotationKey] = policyName
}

// hpaSpecDrifted compares only the fields the operator controls to avoid
// unnecessary updates caused by server-side defaults that reflect.DeepEqual
// would detect as changes.
func hpaSpecDrifted(existing, desired *autoscalingv2.HorizontalPodAutoscaler) bool {
	if existing.Spec.ScaleTargetRef != desired.Spec.ScaleTargetRef {
		return true
	}
	if existing.Spec.MaxReplicas != desired.Spec.MaxReplicas {
		return true
	}
	existingMin := int32(0)
	if existing.Spec.MinReplicas != nil {
		existingMin = *existing.Spec.MinReplicas
	}
	desiredMin := int32(0)
	if desired.Spec.MinReplicas != nil {
		desiredMin = *desired.Spec.MinReplicas
	}
	if existingMin != desiredMin {
		return true
	}
	if len(existing.Spec.Metrics) != len(desired.Spec.Metrics) {
		return true
	}
	for i := range desired.Spec.Metrics {
		if i >= len(existing.Spec.Metrics) {
			return true
		}
		em := existing.Spec.Metrics[i]
		dm := desired.Spec.Metrics[i]
		if em.Type != dm.Type {
			return true
		}
		if em.Resource == nil || dm.Resource == nil {
			if em.Resource != dm.Resource {
				return true
			}
			continue
		}
		if em.Resource.Name != dm.Resource.Name {
			return true
		}
		if em.Resource.Target.Type != dm.Resource.Target.Type {
			return true
		}
		existingUtil := int32(0)
		if em.Resource.Target.AverageUtilization != nil {
			existingUtil = *em.Resource.Target.AverageUtilization
		}
		desiredUtil := int32(0)
		if dm.Resource.Target.AverageUtilization != nil {
			desiredUtil = *dm.Resource.Target.AverageUtilization
		}
		if existingUtil != desiredUtil {
			return true
		}
	}
	return false
}
