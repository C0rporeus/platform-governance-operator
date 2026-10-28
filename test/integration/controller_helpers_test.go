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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func expectAvailableCondition(conditions []metav1.Condition) {
	availableCondition := meta.FindStatusCondition(conditions, "Available")
	Expect(availableCondition).NotTo(BeNil())
	Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
}

// controllerSmokeSpec describes a Reconcile() smoke test shared by every CRD controller.
type controllerSmokeSpec struct {
	// Kind is the CRD kind, used in the Describe title and step names.
	Kind string
	// ResourceName is the name of the object created for the test.
	ResourceName string
	// NewObject returns an empty instance of the CRD.
	NewObject func() client.Object
	// NewReconciler returns the controller under test.
	NewReconciler func() reconcile.Reconciler
	// Conditions extracts status conditions from an instance returned by NewObject.
	Conditions func(client.Object) []metav1.Condition
}

// controllerSmokeBody returns the Describe body of a spec that reconciles a default resource twice
// and verifies the Available condition and that the second pass is a no-op.
func controllerSmokeBody(spec controllerSmokeSpec) func() {
	return func() {
		Context("When reconciling a resource", func() {
			ctx := context.Background()
			key := types.NamespacedName{Name: spec.ResourceName, Namespace: "default"}

			BeforeEach(func() {
				By("creating the custom resource for the Kind " + spec.Kind)
				err := k8sClient.Get(ctx, key, spec.NewObject())
				if err != nil && errors.IsNotFound(err) {
					resource := spec.NewObject()
					resource.SetName(key.Name)
					resource.SetNamespace(key.Namespace)
					Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				}
			})

			AfterEach(func() {
				resource := spec.NewObject()
				Expect(k8sClient.Get(ctx, key, resource)).To(Succeed())

				By("Cleanup the specific resource instance " + spec.Kind)
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})

			It("should successfully reconcile the resource", func() {
				By("Reconciling the created resource")
				reconciler := spec.NewReconciler()

				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())

				reconciled := spec.NewObject()
				Expect(k8sClient.Get(ctx, key, reconciled)).To(Succeed())
				expectAvailableCondition(spec.Conditions(reconciled))
				resourceVersionAfterFirstReconcile := reconciled.GetResourceVersion()

				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())

				reconciledAfterSecondRun := spec.NewObject()
				Expect(k8sClient.Get(ctx, key, reconciledAfterSecondRun)).To(Succeed())
				Expect(reconciledAfterSecondRun.GetResourceVersion()).To(Equal(resourceVersionAfterFirstReconcile))
				expectAvailableCondition(spec.Conditions(reconciledAfterSecondRun))
			})
		})
	}
}
