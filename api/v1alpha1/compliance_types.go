package v1alpha1

// ComplianceExample identifies an observed rule violation without copying the Pod spec.
type ComplianceExample struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Rule      string `json:"rule"`
}

// ComplianceSummary describes the latest bounded audit of existing Pods.
// Complete=false means the result cannot establish full compliance.
type ComplianceSummary struct {
	// ObservedGeneration is the baseline generation assessed by this report.
	ObservedGeneration int64 `json:"observedGeneration"`
	// Inspected includes Pods examined but excluded by workload targeting.
	// +kubebuilder:validation:Minimum=0
	Inspected int `json:"inspected"`
	// Evaluated counts applicable Pods evaluated successfully.
	// +kubebuilder:validation:Minimum=0
	Evaluated int `json:"evaluated"`
	// Violating counts Pods with at least one confirmed violation of this baseline.
	// +kubebuilder:validation:Minimum=0
	Violating int  `json:"violating"`
	Complete  bool `json:"complete"`
	// Examples contains at most ten confirmed rule violations.
	// +kubebuilder:validation:MaxItems=10
	// +optional
	Examples []ComplianceExample `json:"examples,omitempty"`
}
