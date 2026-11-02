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
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/f3nr1r/platform-governance-operator/api/v1alpha1"
)

// These specs run against a real API server (envtest), which is where the CRD
// validation rules (CEL, x-kubernetes-validations) and the size limits are enforced.
// No webhooks are installed here, so every rejection comes from the CRD schema itself.
var _ = Describe("CRD validation rules (CEL and size limits)", func() {
	newWorkloadPolicy := func(spec corev1alpha1.WorkloadPolicySpec) *corev1alpha1.WorkloadPolicy {
		return &corev1alpha1.WorkloadPolicy{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "cel-", Namespace: "default"},
			Spec:       spec,
		}
	}

	// create submits obj and, when the API server accepts it, removes it after the spec.
	create := func(obj client.Object) error {
		GinkgoHelper()
		err := k8sClient.Create(ctx, obj)
		if err == nil {
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, obj) })
		}
		return err
	}

	ref := func(group, kind string) *corev1alpha1.PolicyTargetReference {
		return &corev1alpha1.PolicyTargetReference{Group: group, Kind: kind, Name: "x"}
	}

	Describe("horizontalScaling replicas", func() {
		scaling := func(minReplicas, maxReplicas int32) corev1alpha1.WorkloadPolicySpec {
			return corev1alpha1.WorkloadPolicySpec{
				HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{MinReplicas: minReplicas, MaxReplicas: maxReplicas},
			}
		}

		It("rejects maxReplicas below minReplicas", func() {
			err := create(newWorkloadPolicy(scaling(5, 3)))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("maxReplicas must be greater than or equal to minReplicas"))
			Expect(err.Error()).To(ContainSubstring("spec.horizontalScaling.maxReplicas"))
		})

		It("rejects a minReplicas above the defaulted maxReplicas", func() {
			// maxReplicas is omitted, so the CRD defaults it to 10 before the rule runs.
			Expect(create(newWorkloadPolicy(scaling(20, 0)))).NotTo(Succeed())
		})

		It("accepts equal and increasing values", func() {
			Expect(create(newWorkloadPolicy(scaling(3, 3)))).To(Succeed())
			Expect(create(newWorkloadPolicy(scaling(2, 10)))).To(Succeed())
		})
	})

	Describe("targetRef kind and API group", func() {
		DescribeTable("on a WorkloadPolicy",
			func(group, kind string, accepted bool) {
				spec := corev1alpha1.WorkloadPolicySpec{}
				spec.TargetRef = ref(group, kind)
				err := create(newWorkloadPolicy(spec))
				if accepted {
					Expect(err).NotTo(HaveOccurred())
					return
				}
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("kind is not valid for the API group"))
			},
			Entry("Deployment in apps", "apps", "Deployment", true),
			Entry("StatefulSet in apps", "apps", "StatefulSet", true),
			Entry("DaemonSet in apps", "apps", "DaemonSet", true),
			Entry("ReplicaSet in apps", "apps", "ReplicaSet", true),
			Entry("Job in batch", "batch", "Job", true),
			Entry("Pod in the core group", "", "Pod", true),
			Entry("ReplicationController in the core group", "", "ReplicationController", true),
			Entry("third-party group is accepted as is (Argo Rollouts)", "argoproj.io", "Rollout", true),
			Entry("Deployment without group would never match a Pod owner", "", "Deployment", false),
			Entry("Deployment in batch", "batch", "Deployment", false),
			Entry("Pod in apps", "apps", "Pod", false),
			Entry("CronJob owns Jobs, not Pods", "batch", "CronJob", false),
		)

		It("applies to every policy kind because they share PolicyTargeting", func() {
			baseline := &corev1alpha1.SecurityBaseline{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "cel-", Namespace: "default"},
				Spec: corev1alpha1.SecurityBaselineSpec{
					RunAsNonRoot:    true,
					PolicyTargeting: corev1alpha1.PolicyTargeting{TargetRef: ref("", "Deployment")},
				},
			}
			err := create(baseline)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kind is not valid for the API group"))

			profile := &corev1alpha1.TelemetryProfile{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "cel-", Namespace: "default"},
				Spec: corev1alpha1.TelemetryProfileSpec{
					InjectEnvVars:   true,
					PolicyTargeting: corev1alpha1.PolicyTargeting{TargetRef: ref("", "Deployment")},
				},
			}
			Expect(create(profile)).NotTo(Succeed())
		})
	})

	Describe("horizontalScaling needs a Deployment target", func() {
		withScaling := func(targeting corev1alpha1.PolicyTargeting) corev1alpha1.WorkloadPolicySpec {
			return corev1alpha1.WorkloadPolicySpec{
				PolicyTargeting:   targeting,
				HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{EnabledByDefault: true},
			}
		}

		It("rejects it on a StatefulSet target, which would silently never get an HPA", func() {
			err := create(newWorkloadPolicy(withScaling(corev1alpha1.PolicyTargeting{TargetRef: ref("apps", "StatefulSet")})))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("horizontalScaling only applies to a targetRef of kind Deployment"))
		})

		It("accepts it on a Deployment target, a selector, or no targeting", func() {
			deployment := corev1alpha1.PolicyTargeting{TargetRef: ref("apps", "Deployment")}
			Expect(create(newWorkloadPolicy(withScaling(deployment)))).To(Succeed())
			Expect(create(newWorkloadPolicy(withScaling(corev1alpha1.PolicyTargeting{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			})))).To(Succeed())
			Expect(create(newWorkloadPolicy(withScaling(corev1alpha1.PolicyTargeting{})))).To(Succeed())
		})
	})

	Describe("defaultRequests against defaultLimits", func() {
		quantities := func(requests, limits map[string]corev1alpha1.QuantityString) corev1alpha1.WorkloadPolicySpec {
			return corev1alpha1.WorkloadPolicySpec{DefaultRequests: requests, DefaultLimits: limits}
		}

		It("rejects a request above its limit", func() {
			err := create(newWorkloadPolicy(quantities(
				map[string]corev1alpha1.QuantityString{"cpu": "2"},
				map[string]corev1alpha1.QuantityString{"cpu": "500m"},
			)))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("defaultRequests cannot exceed defaultLimits"))
		})

		It("compares quantities, not text: units are normalised", func() {
			Expect(create(newWorkloadPolicy(quantities(
				map[string]corev1alpha1.QuantityString{"memory": "1Gi", "cpu": "500m"},
				map[string]corev1alpha1.QuantityString{"memory": "1024Mi", "cpu": "1"},
			)))).To(Succeed())
			Expect(create(newWorkloadPolicy(quantities(
				map[string]corev1alpha1.QuantityString{"memory": "2Gi"},
				map[string]corev1alpha1.QuantityString{"memory": "1500Mi"},
			)))).NotTo(Succeed())
		})

		It("only compares resources present on both sides", func() {
			Expect(create(newWorkloadPolicy(quantities(
				map[string]corev1alpha1.QuantityString{"cpu": "4"},
				map[string]corev1alpha1.QuantityString{"memory": "1Gi"},
			)))).To(Succeed())
			Expect(create(newWorkloadPolicy(quantities(map[string]corev1alpha1.QuantityString{"cpu": "4"}, nil)))).To(Succeed())
		})

		It("leaves malformed quantities to the validating webhook instead of failing with a CEL error", func() {
			err := create(newWorkloadPolicy(quantities(
				map[string]corev1alpha1.QuantityString{"cpu": "not-a-quantity"},
				map[string]corev1alpha1.QuantityString{"cpu": "1"},
			)))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("size limits", func() {
		many := func(n int, prefix string) []string {
			out := make([]string, n)
			for i := range out {
				out[i] = fmt.Sprintf("%s-%d", prefix, i)
			}
			return out
		}
		baseline := func(spec corev1alpha1.SecurityBaselineSpec) *corev1alpha1.SecurityBaseline {
			spec.RunAsNonRoot = true
			return &corev1alpha1.SecurityBaseline{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "cel-", Namespace: "default"},
				Spec:       spec,
			}
		}

		DescribeTable("rejects oversized lists",
			func(spec corev1alpha1.SecurityBaselineSpec, field string) {
				err := create(baseline(spec))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(field))
			},
			Entry("too many allowedRegistries",
				corev1alpha1.SecurityBaselineSpec{AllowedRegistries: many(65, "registry.example.com/team")}, "allowedRegistries"),
			Entry("allowedRegistries entry too long",
				corev1alpha1.SecurityBaselineSpec{AllowedRegistries: []string{strings.Repeat("a", 256)}}, "allowedRegistries"),
			Entry("too many excludedNamespaces",
				corev1alpha1.SecurityBaselineSpec{ExcludedNamespaces: many(257, "ns")}, "excludedNamespaces"),
			Entry("excludedNamespaces entry longer than a namespace name",
				corev1alpha1.SecurityBaselineSpec{ExcludedNamespaces: []string{strings.Repeat("n", 64)}}, "excludedNamespaces"),
		)

		It("accepts lists at the limit", func() {
			Expect(create(baseline(corev1alpha1.SecurityBaselineSpec{
				AllowedRegistries:  many(64, "registry.example.com/team"),
				ExcludedNamespaces: many(256, "ns"),
			}))).To(Succeed())
		})

		It("rejects too many allowedCapabilities", func() {
			capabilities := make([]string, 65)
			for i := range capabilities {
				capabilities[i] = fmt.Sprintf("CAP_%d", i)
			}
			spec := corev1alpha1.SecurityBaselineSpec{}
			for _, c := range capabilities {
				spec.AllowedCapabilities = append(spec.AllowedCapabilities, corev1.Capability(c))
			}
			Expect(create(baseline(spec))).NotTo(Succeed())
		})

		It("rejects oversized resource and label maps", func() {
			tooManyResources := map[string]corev1alpha1.QuantityString{}
			for i := range 17 {
				tooManyResources[fmt.Sprintf("example.com/device-%d", i)] = "1"
			}
			tooManyRequests := corev1alpha1.WorkloadPolicySpec{DefaultRequests: tooManyResources}
			Expect(create(newWorkloadPolicy(tooManyRequests))).NotTo(Succeed())

			longQuantity := corev1alpha1.QuantityString(strings.Repeat("1", 65))
			Expect(create(newWorkloadPolicy(corev1alpha1.WorkloadPolicySpec{
				DefaultLimits: map[string]corev1alpha1.QuantityString{"cpu": longQuantity},
			}))).NotTo(Succeed())

			tooManyLabels := map[string]corev1alpha1.LabelValueString{}
			for i := range 33 {
				tooManyLabels[fmt.Sprintf("label-%d", i)] = "v"
			}
			Expect(create(newWorkloadPolicy(corev1alpha1.WorkloadPolicySpec{MandatoryLabels: tooManyLabels}))).NotTo(Succeed())

			longLabelValue := corev1alpha1.LabelValueString(strings.Repeat("v", 64))
			Expect(create(newWorkloadPolicy(corev1alpha1.WorkloadPolicySpec{
				MandatoryLabels: map[string]corev1alpha1.LabelValueString{"team": longLabelValue},
			}))).NotTo(Succeed())
		})
	})

	It("keeps accepting updates of a valid object (rules must not strand stored policies)", func() {
		wp := newWorkloadPolicy(corev1alpha1.WorkloadPolicySpec{
			PolicyTargeting:   corev1alpha1.PolicyTargeting{TargetRef: ref("apps", "Deployment")},
			DefaultRequests:   map[string]corev1alpha1.QuantityString{"cpu": "100m"},
			DefaultLimits:     map[string]corev1alpha1.QuantityString{"cpu": "500m"},
			HorizontalScaling: &corev1alpha1.HorizontalScalingPolicy{MinReplicas: 2, MaxReplicas: 6},
		})
		Expect(create(wp)).To(Succeed())

		wp.Spec.Priority = 5
		Expect(k8sClient.Update(ctx, wp)).To(Succeed())

		wp.Spec.HorizontalScaling.MaxReplicas = 1
		Expect(k8sClient.Update(ctx, wp)).NotTo(Succeed(), "an update that breaks a rule is still rejected")
	})
})
