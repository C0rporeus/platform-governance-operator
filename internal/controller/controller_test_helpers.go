package controller

import (
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func expectAvailableCondition(conditions []metav1.Condition) {
	availableCondition := meta.FindStatusCondition(conditions, "Available")
	gomega.Expect(availableCondition).NotTo(gomega.BeNil())
	gomega.Expect(availableCondition.Status).To(gomega.Equal(metav1.ConditionTrue))
}
