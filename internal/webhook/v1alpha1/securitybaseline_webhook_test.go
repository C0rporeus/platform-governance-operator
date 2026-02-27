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

var _ = Describe("SecurityBaseline Webhook", func() {
	var (
		obj       *corev1alpha1.SecurityBaseline
		oldObj    *corev1alpha1.SecurityBaseline
		validator SecurityBaselineCustomValidator
		defaulter SecurityBaselineCustomDefaulter
	)

	BeforeEach(func() {
		obj = &corev1alpha1.SecurityBaseline{}
		oldObj = &corev1alpha1.SecurityBaseline{}
		validator = SecurityBaselineCustomValidator{}
		Expect(validator).NotTo(BeNil())
		defaulter = SecurityBaselineCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil())
		Expect(oldObj).NotTo(BeNil())
		Expect(obj).NotTo(BeNil())
	})

	Context("When creating SecurityBaseline under Defaulting Webhook", func() {
		It("Should apply defaults without error", func() {
			Expect(defaulter.Default(ctx, obj)).To(Succeed())
		})
	})

	Context("When creating or updating SecurityBaseline under Validating Webhook", func() {
		It("Should admit a valid SecurityBaseline", func() {
			obj.Spec.RunAsNonRoot = true
			obj.Spec.ReadOnlyRootFilesystem = true
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny creation with an empty excludedNamespaces entry", func() {
			obj.Spec.ExcludedNamespaces = []string{""}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("Should admit a valid update", func() {
			obj.Spec.RunAsNonRoot = true
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should admit deletion", func() {
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
