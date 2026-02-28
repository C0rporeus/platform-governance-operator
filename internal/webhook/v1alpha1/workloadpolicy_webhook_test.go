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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

var _ = Describe("WorkloadPolicy Webhook", func() {
	var (
		obj       *corev1alpha1.WorkloadPolicy
		oldObj    *corev1alpha1.WorkloadPolicy
		validator WorkloadPolicyCustomValidator
		defaulter WorkloadPolicyCustomDefaulter
	)

	BeforeEach(func() {
		obj = &corev1alpha1.WorkloadPolicy{}
		oldObj = &corev1alpha1.WorkloadPolicy{}
		validator = WorkloadPolicyCustomValidator{}
		Expect(validator).NotTo(BeNil())
		defaulter = WorkloadPolicyCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil())
		Expect(oldObj).NotTo(BeNil())
		Expect(obj).NotTo(BeNil())
	})

	Context("When creating WorkloadPolicy under Defaulting Webhook", func() {
		It("Should apply defaults without error", func() {
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
		})

		It("Should default horizontal scaling values when configured", func() {
			obj.Spec.HorizontalScaling = &corev1alpha1.HorizontalScalingPolicy{}
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.HorizontalScaling.MinReplicas).To(Equal(int32(2)))
			Expect(obj.Spec.HorizontalScaling.MaxReplicas).To(Equal(int32(10)))
			Expect(obj.Spec.HorizontalScaling.TargetCPUUtilizationPercentage).To(Equal(int32(80)))
		})
	})

	Context("When creating or updating WorkloadPolicy under Validating Webhook", func() {
		It("Should admit a valid WorkloadPolicy with no requests or limits", func() {
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation with an invalid defaultRequests quantity", func() {
			obj.Spec.DefaultRequests = map[string]string{"cpu": "not-a-quantity"}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should deny creation with an invalid defaultLimits quantity", func() {
			obj.Spec.DefaultLimits = map[string]string{"memory": "not-a-quantity"}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should deny creation with an empty mandatoryLabels key", func() {
			obj.Spec.MandatoryLabels = map[string]string{"": "value"}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should deny creation with an empty mandatoryLabels value", func() {
			obj.Spec.MandatoryLabels = map[string]string{"key": ""}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should admit a valid update", func() {
			obj.Spec.DefaultRequests = map[string]string{"cpu": "100m"}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation when horizontalScaling maxReplicas is lower than minReplicas", func() {
			obj.Spec.HorizontalScaling = &corev1alpha1.HorizontalScalingPolicy{
				MinReplicas: 5,
				MaxReplicas: 2,
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should deny creation when horizontalScaling target CPU is out of range", func() {
			obj.Spec.HorizontalScaling = &corev1alpha1.HorizontalScalingPolicy{
				MinReplicas:                    1,
				MaxReplicas:                    3,
				TargetCPUUtilizationPercentage: 101,
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should admit a valid horizontalScaling configuration", func() {
			obj.Spec.HorizontalScaling = &corev1alpha1.HorizontalScalingPolicy{
				EnabledByDefault:               true,
				MinReplicas:                    2,
				MaxReplicas:                    8,
				TargetCPUUtilizationPercentage: 70,
			}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit deletion", func() {
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
