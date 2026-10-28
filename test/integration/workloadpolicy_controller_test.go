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

package integration

import (
	. "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
	"github.com/f3nr1r/platform-governance-operator/internal/controller"
)

var _ = Describe("WorkloadPolicy Controller", controllerSmokeBody(controllerSmokeSpec{
	Kind:         "WorkloadPolicy",
	ResourceName: "test-workloadpolicy",
	NewObject:    func() client.Object { return &corev1alpha1.WorkloadPolicy{} },
	NewReconciler: func() reconcile.Reconciler {
		return &controller.WorkloadPolicyReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10),
		}
	},
	Conditions: func(o client.Object) []metav1.Condition { return o.(*corev1alpha1.WorkloadPolicy).Status.Conditions },
}))
