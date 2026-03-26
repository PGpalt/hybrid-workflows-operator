package compiler

import (
	"encoding/json"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hybridwfv1alpha1 "github.com/PGpalt/hybrid-workflows-operator/api/v1alpha1"
)

func TestCompileBuildsWorkflowFromMixedJobs(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample",
			Namespace: "default",
		},
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "generate",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{
							"image":   "busybox",
							"command": []string{"sh", "-c"},
							"args":    []string{"echo hello >/tmp/file.txt"},
						},
						"outputs": map[string]any{
							"parameters": []any{
								map[string]any{
									"name": "output-param-1",
									"valueFrom": map[string]any{
										"path": "/tmp/file.txt",
									},
								},
							},
							"artifacts": []any{
								map[string]any{
									"name": "file",
									"path": "/tmp/file.txt",
								},
							},
						},
					}),
				},
				{
					Name:    "model-training",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{From: "generate.file"},
					},
					Outputs: []hybridwfv1alpha1.HybridWorkflowOutput{
						{Name: "outputFileName", Value: "mnist.log"},
					},
				},
				{
					Name: "print-message",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"inputs": map[string]any{
							"artifacts": []any{
								map[string]any{
									"name": "message",
									"path": "/tmp/message",
								},
							},
						},
						"container": map[string]any{
							"image":   "alpine",
							"command": []string{"sh", "-c"},
							"args":    []string{"cat /tmp/message/mnist.log"},
						},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{
							Name: "message",
							From: "model-training",
							Type: hybridwfv1alpha1.HybridWorkflowInputTypeArtifact,
						},
					},
				},
			},
		},
	}

	workflow, err := Compile(hw)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	if workflow.Spec.Entrypoint != "hybrid-workflow" {
		t.Fatalf("unexpected entrypoint: %s", workflow.Spec.Entrypoint)
	}
	if len(workflow.Spec.Templates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(workflow.Spec.Templates))
	}

	dag := workflow.Spec.Templates[0].DAG
	if dag == nil {
		t.Fatalf("expected DAG template at index 0")
	}
	if len(dag.Tasks) != 3 {
		t.Fatalf("expected 3 DAG tasks, got %d", len(dag.Tasks))
	}

	modelTraining := dag.Tasks[1]
	if modelTraining.TemplateRef == nil || modelTraining.TemplateRef.Name != "slurm-template" {
		t.Fatalf("expected slurm templateRef, got %#v", modelTraining.TemplateRef)
	}
	if len(modelTraining.Arguments.Parameters) == 0 {
		t.Fatalf("expected slurm task parameters")
	}

	printTask := dag.Tasks[2]
	if len(printTask.Arguments.Artifacts) != 1 {
		t.Fatalf("expected print task to receive one artifact, got %d", len(printTask.Arguments.Artifacts))
	}
	if printTask.Arguments.Artifacts[0].From == "" {
		t.Fatalf("expected artifact 'from' reference to be set")
	}
}

func TestCompileAddsCleanupTaskForDependentSlurmJobs(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name:    "stage",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "prepare",
					Outputs: []hybridwfv1alpha1.HybridWorkflowOutput{
						{Name: "cleanDataPath", Value: "shared-dir"},
					},
				},
				{
					Name:    "consume",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "consume",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{From: "stage"},
					},
				},
			},
		},
	}

	workflow, err := Compile(hw)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	dag := workflow.Spec.Templates[0].DAG
	if dag == nil {
		t.Fatalf("expected DAG template")
	}
	if len(dag.Tasks) != 3 {
		t.Fatalf("expected cleanup task to be appended, got %d tasks", len(dag.Tasks))
	}
	if dag.Tasks[2].Name != "stage-cleanup" {
		t.Fatalf("expected cleanup task name stage-cleanup, got %s", dag.Tasks[2].Name)
	}
}

func TestCompileRejectsCycles(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "a",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{Name: "x", From: "b"},
					},
				},
				{
					Name: "b",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{Name: "y", From: "a"},
					},
				},
			},
		},
	}

	if _, err := Compile(hw); err == nil {
		t.Fatalf("expected cycle validation error")
	}
}

func TestCompileRejectsAmbiguousBareArtifactReference(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "generate",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "a", "path": "/tmp/a"},
								map[string]any{"name": "b", "path": "/tmp/b"},
							},
						},
					}),
				},
				{
					Name: "consume",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"inputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "data", "path": "/tmp/data"},
							},
						},
						"container": map[string]any{"image": "busybox"},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{Name: "data", From: "generate", Type: hybridwfv1alpha1.HybridWorkflowInputTypeArtifact},
					},
				},
			},
		},
	}

	assertCompileErrorContains(t, hw, "does not expose exactly one artifact")
}

func TestCompileRejectsDuplicateK8sInputNames(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "consume",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{Name: "message", Value: stringPtr("hello")},
						{Name: "message", Value: stringPtr("world")},
					},
				},
			},
		},
	}

	assertCompileErrorContains(t, hw, `duplicate input name "message"`)
}

func TestCompileAllowsSlurmJobsWithLiteralS3KeyAndDependencyOnlyInputs(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "prepare",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "workspace", "path": "/tmp/workspace"},
							},
						},
					}),
				},
				{
					Name:    "stage",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch stage.slurm",
				},
				{
					Name:    "train",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{S3Key: "mnist", Path: "shared-prefix"},
						{From: "prepare.workspace"},
						{From: "stage"},
					},
				},
			},
		},
	}

	workflow, err := Compile(hw)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	dag := workflow.Spec.Templates[0].DAG
	if dag == nil {
		t.Fatalf("expected DAG template")
	}
	if len(dag.Tasks) != 3 {
		t.Fatalf("expected 3 DAG tasks, got %d", len(dag.Tasks))
	}

	trainTask := dag.Tasks[2]
	if len(trainTask.Dependencies) != 2 {
		t.Fatalf("expected dependency-only inputs to create 2 dependencies, got %d", len(trainTask.Dependencies))
	}
	if len(trainTask.Arguments.Artifacts) != 0 {
		t.Fatalf("expected no direct artifacts when s3key is authoritative, got %d", len(trainTask.Arguments.Artifacts))
	}

	values := map[string]string{}
	for _, param := range trainTask.Arguments.Parameters {
		values[param.Name] = param.GetValue()
	}

	if got := values["s3artifact"]; got != "mnist" {
		t.Fatalf("expected s3artifact parameter to be mnist, got %q", got)
	}
	if got := values["inputFilePath"]; got != "shared-prefix" {
		t.Fatalf("expected inputFilePath parameter to be shared-prefix, got %q", got)
	}
	if _, ok := values["slurmInput"]; ok {
		t.Fatalf("did not expect slurmInput parameter when s3key is authoritative")
	}
}

func TestCompileRejectsMultipleFromInputsWithoutS3KeyForSlurmJobs(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "prepare-a",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "workspace", "path": "/tmp/workspace"},
							},
						},
					}),
				},
				{
					Name: "prepare-b",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "workspace", "path": "/tmp/workspace"},
							},
						},
					}),
				},
				{
					Name:    "train",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{From: "prepare-a.workspace"},
						{From: "prepare-b.workspace"},
					},
				},
			},
		},
	}

	assertCompileErrorContains(t, hw, "may define at most one from input when no s3key input is present")
}

func TestCompileAllowsSlurmJobsWithLiteralS3KeyInput(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name:    "stage",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch stage.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{S3Key: "GenomicData", Path: "input-dir"},
					},
				},
			},
		},
	}

	workflow, err := Compile(hw)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	dag := workflow.Spec.Templates[0].DAG
	if dag == nil {
		t.Fatalf("expected DAG template")
	}
	if len(dag.Tasks) != 1 {
		t.Fatalf("expected 1 DAG task, got %d", len(dag.Tasks))
	}

	task := dag.Tasks[0]
	if task.TemplateRef == nil || task.TemplateRef.Name != "slurm-template" {
		t.Fatalf("expected slurm templateRef, got %#v", task.TemplateRef)
	}

	values := map[string]string{}
	for _, param := range task.Arguments.Parameters {
		values[param.Name] = param.GetValue()
	}

	if got := values["s3artifact"]; got != "GenomicData" {
		t.Fatalf("expected s3artifact parameter to be GenomicData, got %q", got)
	}
	if got := values["inputFilePath"]; got != "input-dir" {
		t.Fatalf("expected inputFilePath parameter to be input-dir, got %q", got)
	}
	if _, ok := values["slurmInput"]; ok {
		t.Fatalf("did not expect slurmInput parameter for pure s3key input")
	}
}

func TestCompileUsesUpstreamK8sArtifactS3KeyForSlurmInput(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "prepare",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{
									"name": "workspace",
									"path": "/tmp/workspace",
									"s3": map[string]any{
										"key": "GenomicDataAris",
									},
								},
							},
						},
					}),
				},
				{
					Name:    "train",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{From: "prepare.workspace"},
					},
				},
			},
		},
	}

	workflow, err := Compile(hw)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	dag := workflow.Spec.Templates[0].DAG
	if dag == nil {
		t.Fatalf("expected DAG template")
	}
	if len(dag.Tasks) != 2 {
		t.Fatalf("expected 2 DAG tasks, got %d", len(dag.Tasks))
	}

	trainTask := dag.Tasks[1]
	if len(trainTask.Arguments.Artifacts) != 0 {
		t.Fatalf("expected slurm task to rely on template s3 input, got %d direct artifacts", len(trainTask.Arguments.Artifacts))
	}

	foundS3Artifact := false
	for _, param := range trainTask.Arguments.Parameters {
		if param.Name != "s3artifact" {
			continue
		}
		foundS3Artifact = true
		if got := param.GetValue(); got != "GenomicDataAris" {
			t.Fatalf("expected s3artifact parameter to be GenomicDataAris, got %q", got)
		}
	}
	if !foundS3Artifact {
		t.Fatalf("expected slurm task to receive s3artifact parameter from upstream k8s output")
	}
}

func TestCompileRejectsK8sParameterInputFromSlurmSource(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name:    "train",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
				},
				{
					Name: "report",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"inputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "logs", "path": "/tmp/logs"},
							},
						},
						"container": map[string]any{"image": "alpine"},
					}),
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{Name: "logs", From: "train"},
					},
				},
			},
		},
	}

	assertCompileErrorContains(t, hw, "must use type=artifact when referencing slurm job")
}

func TestCompileRejectsUnknownExplicitArtifactOutput(t *testing.T) {
	hw := &hybridwfv1alpha1.HybridWorkflow{
		Spec: hybridwfv1alpha1.HybridWorkflowSpec{
			Jobs: []hybridwfv1alpha1.HybridWorkflowJob{
				{
					Name: "prepare",
					Type: hybridwfv1alpha1.HybridWorkflowJobTypeK8s,
					JobSpec: mustJSON(t, map[string]any{
						"container": map[string]any{"image": "busybox"},
						"outputs": map[string]any{
							"artifacts": []any{
								map[string]any{"name": "workspace", "path": "/tmp/workspace"},
							},
						},
					}),
				},
				{
					Name:    "train",
					Type:    hybridwfv1alpha1.HybridWorkflowJobTypeSlurm,
					Command: "sbatch train.slurm",
					Inputs: []hybridwfv1alpha1.HybridWorkflowInput{
						{From: "prepare.missing"},
					},
				},
			},
		},
	}

	assertCompileErrorContains(t, hw, `references unknown artifact output "missing"`)
}

func assertCompileErrorContains(t *testing.T, hw *hybridwfv1alpha1.HybridWorkflow, want string) {
	t.Helper()

	_, err := Compile(hw)
	if err == nil {
		t.Fatalf("expected compile error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected compile error containing %q, got %q", want, err.Error())
	}
}

func mustJSON(t *testing.T, value any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func stringPtr(value string) *string {
	return &value
}
