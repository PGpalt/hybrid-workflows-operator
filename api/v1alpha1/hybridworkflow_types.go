package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HybridWorkflowJobType identifies the execution backend for a job.
type HybridWorkflowJobType string

const (
	HybridWorkflowJobTypeK8s   HybridWorkflowJobType = "k8s"
	HybridWorkflowJobTypeSlurm HybridWorkflowJobType = "slurm"
)

// HybridWorkflowInputType identifies how a k8s input is consumed.
type HybridWorkflowInputType string

const (
	HybridWorkflowInputTypeParameter HybridWorkflowInputType = "parameter"
	HybridWorkflowInputTypeArtifact  HybridWorkflowInputType = "artifact"
)

// HybridWorkflowPhase describes the controller-observed lifecycle phase.
type HybridWorkflowPhase string

const (
	HybridWorkflowPhasePending   HybridWorkflowPhase = "Pending"
	HybridWorkflowPhaseRendering HybridWorkflowPhase = "Rendering"
	HybridWorkflowPhaseRendered  HybridWorkflowPhase = "Rendered"
	HybridWorkflowPhaseSubmitted HybridWorkflowPhase = "Submitted"
	HybridWorkflowPhaseRunning   HybridWorkflowPhase = "Running"
	HybridWorkflowPhaseSucceeded HybridWorkflowPhase = "Succeeded"
	HybridWorkflowPhaseFailed    HybridWorkflowPhase = "Failed"
	HybridWorkflowPhaseError     HybridWorkflowPhase = "Error"
)

// HybridWorkflowSpec defines the desired high-level hybrid workflow definition.
type HybridWorkflowSpec struct {
	// Jobs is the ordered list of hybrid workflow jobs.
	// +kubebuilder:validation:MinItems=1
	Jobs []HybridWorkflowJob `json:"jobs"`
}

// HybridWorkflowJob is a single DSL job node that compiles into either an Argo
// template or a slurm templateRef task.
// +kubebuilder:validation:XValidation:rule="self.type != 'k8s' || has(self.jobSpec)",message="k8s jobs require jobSpec"
// +kubebuilder:validation:XValidation:rule="self.type != 'slurm' || has(self.command)",message="slurm jobs require command"
// +kubebuilder:validation:XValidation:rule="self.type != 'slurm' || !has(self.jobSpec)",message="slurm jobs must not define jobSpec"
type HybridWorkflowJob struct {
	// Command is passed to the shared slurm template and is required for slurm jobs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Command string `json:"command,omitempty"`

	// Inputs are upstream references or literals used to build task arguments.
	// +optional
	Inputs []HybridWorkflowInput `json:"inputs,omitempty"`

	// JobSpec is an arbitrary Argo template fragment for k8s jobs.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	JobSpec *apiextensionsv1.JSON `json:"jobSpec,omitempty"`

	// Name is the unique job name inside the HybridWorkflow.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9-]*$`
	Name string `json:"name"`

	// Outputs are extra named values propagated into slurm task parameters.
	// +optional
	Outputs []HybridWorkflowOutput `json:"outputs,omitempty"`

	// Template is an optional template name override for k8s jobs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Template string `json:"template,omitempty"`

	// Type is the execution backend for this job.
	// +kubebuilder:validation:Enum=k8s;slurm
	Type HybridWorkflowJobType `json:"type"`
}

// HybridWorkflowInput is an upstream reference or literal used to build task arguments.
// +kubebuilder:validation:XValidation:rule="!has(self.path) || has(self.s3key)",message="input.path requires input.s3key"
type HybridWorkflowInput struct {
	// From is the upstream source in the form jobName or jobName.outputName.
	// +optional
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9][A-Za-z0-9_.-]*)?$`
	From string `json:"from,omitempty"`

	// Name is the input name for k8s consumers and is ignored for slurm consumers.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Path is the local input file path paired with s3key for slurm jobs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path,omitempty"`

	// S3Key is a literal S3 key used only by slurm jobs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	S3Key string `json:"s3key,omitempty"`

	// Type controls whether a k8s input consumes a parameter or an artifact.
	// +optional
	// +kubebuilder:default=parameter
	// +kubebuilder:validation:Enum=parameter;artifact
	Type HybridWorkflowInputType `json:"type,omitempty"`

	// Value is a literal used only by k8s jobs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Value *string `json:"value,omitempty"`
}

// HybridWorkflowOutput is an extra named value forwarded by the compiler.
type HybridWorkflowOutput struct {
	// Name is the output parameter name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value is the output value forwarded by the compiler.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// HybridWorkflowStatus defines the observed state from the controller and rendered Argo Workflow.
type HybridWorkflowStatus struct {
	// Conditions represent the current reconciliation state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []HybridWorkflowCondition `json:"conditions,omitempty"`

	// ObservedGeneration is the last reconciled generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is the high-level state of the rendered workflow.
	// +optional
	// +kubebuilder:validation:Enum=Pending;Rendering;Rendered;Submitted;Running;Succeeded;Failed;Error
	Phase HybridWorkflowPhase `json:"phase,omitempty"`

	// RenderedWorkflowName is the name of the Argo Workflow created from this resource.
	// +optional
	RenderedWorkflowName string `json:"renderedWorkflowName,omitempty"`
}

// HybridWorkflowCondition captures one status condition entry.
type HybridWorkflowCondition struct {
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// +optional
	Message string `json:"message,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Reason string `json:"reason"`

	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`

	Type string `json:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=hybridworkflows,scope=Namespaced,shortName=hwf
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Workflow",type=string,JSONPath=".status.renderedWorkflowName"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// HybridWorkflow stores the high-level DSL that is compiled into an Argo Workflow.
type HybridWorkflow struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec HybridWorkflowSpec `json:"spec"`

	// +optional
	Status HybridWorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HybridWorkflowList contains a list of HybridWorkflow resources.
type HybridWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HybridWorkflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HybridWorkflow{}, &HybridWorkflowList{})
}
