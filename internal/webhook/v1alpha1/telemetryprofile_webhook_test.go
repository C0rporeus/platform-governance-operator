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

var _ = Describe("TelemetryProfile Webhook", func() {
	var (
		obj       *corev1alpha1.TelemetryProfile
		oldObj    *corev1alpha1.TelemetryProfile
		validator TelemetryProfileCustomValidator
		defaulter TelemetryProfileCustomDefaulter
	)

	BeforeEach(func() {
		obj = &corev1alpha1.TelemetryProfile{}
		oldObj = &corev1alpha1.TelemetryProfile{}
		validator = TelemetryProfileCustomValidator{}
		Expect(validator).NotTo(BeNil())
		defaulter = TelemetryProfileCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil())
		Expect(oldObj).NotTo(BeNil())
		Expect(obj).NotTo(BeNil())
	})

	Context("When creating TelemetryProfile under Defaulting Webhook", func() {
		It("Should apply defaults without error", func() {
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
		})
	})

	Context("When creating or updating TelemetryProfile under Validating Webhook", func() {
		It("Should admit a valid TelemetryProfile with no fields set", func() {
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit a valid tracingEndpoint", func() {
			obj.Spec.TracingEndpoint = "http://otel-collector:4318"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation with an invalid tracingEndpoint", func() {
			obj.Spec.TracingEndpoint = "not-a-url"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should admit a valid samplingRate", func() {
			obj.Spec.SamplingRate = "0.5"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation with a samplingRate out of range", func() {
			obj.Spec.SamplingRate = "1.5"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should deny creation with a non-numeric samplingRate", func() {
			obj.Spec.SamplingRate = "high"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should admit a valid update", func() {
			obj.Spec.SamplingRate = "1.0"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit deletion", func() {
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
