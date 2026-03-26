package compiler

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	hybridwfv1alpha1 "github.com/PGpalt/hybrid-workflows-operator/api/v1alpha1"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// Compile renders a HybridWorkflow custom resource into an Argo Workflow.
func Compile(hw *hybridwfv1alpha1.HybridWorkflow) (*wfv1.Workflow, error) {
	if hw == nil {
		return nil, fmt.Errorf("hybrid workflow is nil")
	}
	if len(hw.Spec.Jobs) == 0 {
		return nil, fmt.Errorf("spec.jobs must not be empty")
	}

	jobTypes, jobsByName, err := buildJobIndex(hw.Spec.Jobs)
	if err != nil {
		return nil, err
	}
	if err := validateJobSemantics(jobTypes, jobsByName); err != nil {
		return nil, err
	}

	jobDeps, err := buildJobDependencies(hw.Spec.Jobs, jobsByName)
	if err != nil {
		return nil, err
	}
	if err := validateAcyclic(jobDeps); err != nil {
		return nil, err
	}

	slurmJobNeeds := determineSlurmFetchNeeds(hw.Spec.Jobs, jobDeps)
	workflow := newWorkflowSkeleton()

	dagTasks, additionalTemplates, err := compileWorkflowTasks(hw.Spec.Jobs, jobDeps, slurmJobNeeds, jobTypes, jobsByName)
	if err != nil {
		return nil, err
	}
	populateWorkflowTemplates(workflow, dagTasks, additionalTemplates)

	if err := applySlurmCleanup(workflow, hw.Spec.Jobs); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(workflow)
	if err != nil {
		return nil, fmt.Errorf("marshal compiled workflow: %w", err)
	}

	var compiled wfv1.Workflow
	if err := json.Unmarshal(raw, &compiled); err != nil {
		return nil, fmt.Errorf("unmarshal compiled workflow: %w", err)
	}

	return &compiled, nil
}

func buildJobIndex(
	jobs []hybridwfv1alpha1.HybridWorkflowJob,
) (map[string]hybridwfv1alpha1.HybridWorkflowJobType, map[string]*hybridwfv1alpha1.HybridWorkflowJob, error) {
	jobTypes := make(map[string]hybridwfv1alpha1.HybridWorkflowJobType, len(jobs))
	jobsByName := make(map[string]*hybridwfv1alpha1.HybridWorkflowJob, len(jobs))

	for i := range jobs {
		job := &jobs[i]
		if _, exists := jobsByName[job.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate job name: %s", job.Name)
		}
		jobTypes[job.Name] = job.Type
		jobsByName[job.Name] = job
	}

	return jobTypes, jobsByName, nil
}

func buildJobDependencies(
	jobs []hybridwfv1alpha1.HybridWorkflowJob,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
) (map[string]map[string]struct{}, error) {
	jobDeps := make(map[string]map[string]struct{}, len(jobs))

	for _, job := range jobs {
		jobDeps[job.Name] = map[string]struct{}{}
		for _, input := range job.Inputs {
			if input.From == "" {
				continue
			}

			sourceJob, _ := splitSource(input.From)
			if _, exists := jobsByName[sourceJob]; !exists {
				return nil, fmt.Errorf("job %q references unknown source job %q", job.Name, sourceJob)
			}
			jobDeps[job.Name][sourceJob] = struct{}{}
		}
	}

	return jobDeps, nil
}

func determineSlurmFetchNeeds(
	jobs []hybridwfv1alpha1.HybridWorkflowJob,
	jobDeps map[string]map[string]struct{},
) map[string]bool {
	slurmJobNeeds := make(map[string]bool)
	for _, job := range jobs {
		if job.Type == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			slurmJobNeeds[job.Name] = false
		}
	}

	for _, job := range jobs {
		if job.Type == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			continue
		}
		for dep := range jobDeps[job.Name] {
			if _, ok := slurmJobNeeds[dep]; ok {
				slurmJobNeeds[dep] = true
			}
		}
	}

	return slurmJobNeeds
}

func newWorkflowSkeleton() map[string]any {
	return map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]any{
			"generateName": "hybrid-workflow-",
		},
		"spec": map[string]any{
			"entrypoint": "hybrid-workflow",
			"templates": []any{
				map[string]any{
					"name": "hybrid-workflow",
					"dag": map[string]any{
						"tasks": []any{},
					},
				},
			},
		},
	}
}

func compileWorkflowTasks(
	jobs []hybridwfv1alpha1.HybridWorkflowJob,
	jobDeps map[string]map[string]struct{},
	slurmJobNeeds map[string]bool,
	jobTypes map[string]hybridwfv1alpha1.HybridWorkflowJobType,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
) ([]any, []any, error) {
	dagTasks := make([]any, 0, len(jobs))
	additionalTemplates := make([]any, 0, len(jobs))
	usedTemplates := map[string]struct{}{}

	for i := range jobs {
		job := &jobs[i]
		task, templateDef, err := compileJobTask(job, sortedKeys(jobDeps[job.Name]), slurmJobNeeds[job.Name], jobTypes, jobsByName, usedTemplates)
		if err != nil {
			return nil, nil, err
		}
		dagTasks = append(dagTasks, task)
		if templateDef != nil {
			additionalTemplates = append(additionalTemplates, templateDef)
		}
	}

	return dagTasks, additionalTemplates, nil
}

func compileJobTask(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	dependencies []string,
	slurmNeedsFetch bool,
	jobTypes map[string]hybridwfv1alpha1.HybridWorkflowJobType,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
	usedTemplates map[string]struct{},
) (map[string]any, map[string]any, error) {
	task := map[string]any{
		"name": job.Name,
	}
	if len(dependencies) > 0 {
		task["dependencies"] = dependencies
	}

	inputArgs, err := processJobInputs(job, jobTypes, jobsByName)
	if err != nil {
		return nil, nil, err
	}

	taskArgs := map[string]any{}
	var templateDef map[string]any

	switch job.Type {
	case hybridwfv1alpha1.HybridWorkflowJobTypeK8s:
		templateDef, err = configureK8sTask(job, task, usedTemplates)
		if err != nil {
			return nil, nil, err
		}
	case hybridwfv1alpha1.HybridWorkflowJobTypeSlurm:
		if err := configureSlurmTask(job, task, taskArgs, slurmNeedsFetch); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, fmt.Errorf("unsupported job type: %s", job.Type)
	}

	taskArgs = mergeArguments(taskArgs, inputArgs)
	if len(taskArgs) > 0 {
		task["arguments"] = taskArgs
	}

	return task, templateDef, nil
}

func configureK8sTask(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	task map[string]any,
	usedTemplates map[string]struct{},
) (map[string]any, error) {
	if job.JobSpec == nil {
		return nil, fmt.Errorf("k8s job %q requires jobSpec", job.Name)
	}

	templateName := job.Template
	if templateName == "" {
		templateName = fmt.Sprintf("%s-template", job.Name)
	}
	if _, exists := usedTemplates[templateName]; exists {
		return nil, fmt.Errorf("duplicate template name: %s", templateName)
	}
	usedTemplates[templateName] = struct{}{}
	task["template"] = templateName

	templateDef, err := buildK8sTemplate(templateName, job)
	if err != nil {
		return nil, err
	}

	return templateDef, nil
}

func configureSlurmTask(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	task map[string]any,
	taskArgs map[string]any,
	slurmNeedsFetch bool,
) error {
	if job.Command == "" {
		return fmt.Errorf("slurm job %q requires command", job.Name)
	}

	params := []any{
		map[string]any{
			"name":  "command",
			"value": job.Command,
		},
	}
	for _, output := range job.Outputs {
		value, err := decodeJSON(output.Value)
		if err != nil {
			return fmt.Errorf("decode output %q for job %q: %w", output.Name, job.Name, err)
		}
		params = append(params, map[string]any{
			"name":  output.Name,
			"value": value,
		})
	}
	if slurmNeedsFetch && !hasOutput(job.Outputs, "fetchData") {
		params = append(params, map[string]any{
			"name":  "fetchData",
			"value": "true",
		})
	}
	taskArgs["parameters"] = params
	task["templateRef"] = map[string]any{
		"name":         "slurm-template",
		"template":     "slurm-submit-job",
		"clusterScope": true,
	}

	return nil
}

func populateWorkflowTemplates(workflow map[string]any, dagTasks []any, additionalTemplates []any) {
	spec := workflow["spec"].(map[string]any)
	templates := spec["templates"].([]any)
	rootTemplate := templates[0].(map[string]any)
	rootTemplate["dag"].(map[string]any)["tasks"] = dagTasks
	spec["templates"] = append(templates, additionalTemplates...)
}

func buildK8sTemplate(templateName string, job *hybridwfv1alpha1.HybridWorkflowJob) (map[string]any, error) {
	jobSpec, err := decodeJSONObject(job.JobSpec)
	if err != nil {
		return nil, fmt.Errorf("decode jobSpec for %q: %w", job.Name, err)
	}

	template := map[string]any{
		"name": templateName,
	}

	if _, hasContainer := jobSpec["container"]; !hasContainer && hasInlineContainerFields(jobSpec) {
		containerSpec := map[string]any{}
		for _, field := range []string{"image", "command", "args"} {
			if value, ok := jobSpec[field]; ok {
				containerSpec[field] = value
				delete(jobSpec, field)
			}
		}
		jobSpec["container"] = containerSpec
	}

	for key, value := range jobSpec {
		template[key] = value
	}

	return template, nil
}

func processJobInputs(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	jobTypes map[string]hybridwfv1alpha1.HybridWorkflowJobType,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
) (map[string]any, error) {
	args := map[string]any{}
	params := make([]any, 0)
	artifacts := make([]any, 0)

	jobHasS3Key := false
	for _, input := range job.Inputs {
		if input.S3Key != "" {
			jobHasS3Key = true
			break
		}
	}

	s3KeyUsed := false
	for _, input := range job.Inputs {
		if job.Type != hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			if input.Name == "" {
				return nil, fmt.Errorf("k8s job %q requires named inputs", job.Name)
			}

			if input.Value != nil {
				if input.Path != "" {
					return nil, fmt.Errorf("k8s job %q input %q uses path; this is only valid for slurm s3 inputs", job.Name, input.Name)
				}
				value, err := decodeJSON(*input.Value)
				if err != nil {
					return nil, fmt.Errorf("decode literal input %q for job %q: %w", input.Name, job.Name, err)
				}
				params = append(params, map[string]any{
					"name":  input.Name,
					"value": value,
				})
				continue
			}

			if input.S3Key != "" {
				return nil, fmt.Errorf("k8s job %q input %q uses s3key; use value instead", job.Name, input.Name)
			}
			if input.Path != "" {
				return nil, fmt.Errorf("k8s job %q input %q uses path; this is only valid for slurm s3 inputs", job.Name, input.Name)
			}
		}

		if job.Type == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			if input.Value != nil {
				return nil, fmt.Errorf("slurm job %q input %q uses value; use s3key instead", job.Name, input.Name)
			}
			if input.Path != "" && input.S3Key == "" {
				return nil, fmt.Errorf("slurm job %q input %q specifies path without s3key", job.Name, input.Name)
			}
			if input.S3Key != "" {
				if s3KeyUsed {
					return nil, fmt.Errorf("slurm job %q may define at most one s3key input", job.Name)
				}
				s3KeyUsed = true
				params = append(params, map[string]any{
					"name":  "s3artifact",
					"value": input.S3Key,
				})
				if input.Path != "" {
					params = append(params, map[string]any{
						"name":  "inputFilePath",
						"value": input.Path,
					})
				}
				continue
			}
		}

		if input.From == "" {
			return nil, fmt.Errorf("job %q has an input without from/value/s3key", job.Name)
		}

		sourceJob, outputName := splitSource(input.From)
		sourceType := jobTypes[sourceJob]

		if job.Type != hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			if sourceType == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
				artifacts = append(artifacts, map[string]any{
					"name": input.Name,
					"from": fmt.Sprintf("{{tasks.%s.outputs.artifacts.output-artifact}}", sourceJob),
				})
				continue
			}

			if input.Type == hybridwfv1alpha1.HybridWorkflowInputTypeArtifact {
				resolved := resolveOutputName(sourceJob, outputName, jobsByName, true)
				artifacts = append(artifacts, map[string]any{
					"name": input.Name,
					"from": fmt.Sprintf("{{tasks.%s.outputs.artifacts.%s}}", sourceJob, resolved),
				})
				continue
			}

			resolved := resolveOutputName(sourceJob, outputName, jobsByName, false)
			params = append(params, map[string]any{
				"name":  input.Name,
				"value": fmt.Sprintf("{{tasks.%s.outputs.parameters.%s}}", sourceJob, resolved),
			})
			continue
		}

		if sourceType == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			params = append(params, map[string]any{
				"name":  "slurmInput",
				"value": "true",
			})
			artifacts = append(artifacts, map[string]any{
				"name": "input-artifact",
				"from": fmt.Sprintf("{{tasks.%s.outputs.artifacts.output-artifact}}", sourceJob),
			})
			continue
		}

		if jobHasS3Key {
			continue
		}

		resolved := resolveOutputName(sourceJob, outputName, jobsByName, true)
		params = append(params, map[string]any{
			"name":  "slurmInput",
			"value": "false",
		})
		artifacts = append(artifacts, map[string]any{
			"name": "input-artifact",
			"from": fmt.Sprintf("{{tasks.%s.outputs.artifacts.%s}}", sourceJob, resolved),
		})
	}

	if len(params) > 0 {
		args["parameters"] = params
	}
	if len(artifacts) > 0 {
		args["artifacts"] = artifacts
	}

	return args, nil
}

func applySlurmCleanup(workflow map[string]any, jobs []hybridwfv1alpha1.HybridWorkflowJob) error {
	dagTemplate := findDAGTemplate(workflow)
	if dagTemplate == nil {
		return nil
	}

	dag, _ := dagTemplate["dag"].(map[string]any)
	taskItems, _ := dag["tasks"].([]any)

	tasksByName := make(map[string]map[string]any, len(taskItems))
	for _, item := range taskItems {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := task["name"].(string)
		if name == "" {
			continue
		}
		tasksByName[name] = task
	}

	jobTypes := make(map[string]hybridwfv1alpha1.HybridWorkflowJobType, len(jobs))
	for i := range jobs {
		jobTypes[jobs[i].Name] = jobs[i].Type
	}

	slurmTaskNames := make([]string, 0)
	for name := range tasksByName {
		if jobTypes[name] == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
			slurmTaskNames = append(slurmTaskNames, name)
		}
	}
	sort.Strings(slurmTaskNames)

	deps := make(map[string]map[string]struct{}, len(tasksByName))
	for name, task := range tasksByName {
		deps[name] = make(map[string]struct{})
		switch items := task["dependencies"].(type) {
		case []string:
			for _, dep := range items {
				deps[name][dep] = struct{}{}
			}
		case []any:
			for _, item := range items {
				if dep, ok := item.(string); ok {
					deps[name][dep] = struct{}{}
				}
			}
		}
	}

	slurmDirectReverseDeps := make(map[string]map[string]struct{}, len(slurmTaskNames))
	for _, name := range slurmTaskNames {
		slurmDirectReverseDeps[name] = map[string]struct{}{}
	}
	for _, name := range slurmTaskNames {
		for dep := range deps[name] {
			if _, ok := slurmDirectReverseDeps[dep]; ok {
				slurmDirectReverseDeps[dep][name] = struct{}{}
			}
		}
	}

	for _, name := range slurmTaskNames {
		if len(slurmDirectReverseDeps[name]) > 0 {
			continue
		}
		task := tasksByName[name]
		if _, ok := getTaskParameter(task, "cleanDataPath"); !ok {
			continue
		}
		ensureTaskParameter(task, "cleanData", "true")
	}

	taskNames := make(map[string]struct{}, len(tasksByName))
	for name := range tasksByName {
		taskNames[name] = struct{}{}
	}

	for _, name := range slurmTaskNames {
		dependents := slurmDirectReverseDeps[name]
		if len(dependents) == 0 {
			continue
		}

		cleanDataPath, ok := getTaskParameter(tasksByName[name], "cleanDataPath")
		if !ok || cleanDataPath == "" {
			continue
		}

		cleanupName := fmt.Sprintf("%s-cleanup", name)
		if _, exists := taskNames[cleanupName]; exists {
			return fmt.Errorf("cleanup task name conflicts with existing task: %s", cleanupName)
		}

		dependencies := []string{name}
		for dependent := range dependents {
			dependencies = append(dependencies, dependent)
		}
		sort.Strings(dependencies)

		cleanupTask := map[string]any{
			"name":         cleanupName,
			"dependencies": dependencies,
			"arguments": map[string]any{
				"parameters": []any{
					map[string]any{"name": "command", "value": fmt.Sprintf("rm -rf -- %q", cleanDataPath)},
					map[string]any{"name": "slurmInput", "value": "true"},
				},
				"artifacts": []any{
					map[string]any{
						"name": "input-artifact",
						"from": fmt.Sprintf("{{tasks.%s.outputs.artifacts.output-artifact}}", name),
					},
				},
			},
			"templateRef": map[string]any{
				"name":         "slurm-template",
				"template":     "slurm-submit-job",
				"clusterScope": true,
			},
		}

		taskItems = append(taskItems, cleanupTask)
		taskNames[cleanupName] = struct{}{}
	}

	dag["tasks"] = taskItems
	return nil
}

func validateAcyclic(jobDeps map[string]map[string]struct{}) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}

	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("cyclic dependency detected involving %q", name)
		}

		visiting[name] = true
		for dep := range jobDeps[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}

	for name := range jobDeps {
		if err := visit(name); err != nil {
			return err
		}
	}

	return nil
}

type k8sOutputInfo struct {
	artifacts  map[string]struct{}
	parameters map[string]struct{}
}

func (info k8sOutputInfo) artifactCount() int {
	return len(info.artifacts)
}

func (info k8sOutputInfo) hasArtifact(name string) bool {
	_, ok := info.artifacts[name]
	return ok
}

func (info k8sOutputInfo) hasParameter(name string) bool {
	_, ok := info.parameters[name]
	return ok
}

func validateJobSemantics(
	jobTypes map[string]hybridwfv1alpha1.HybridWorkflowJobType,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
) error {
	outputsByJob := make(map[string]k8sOutputInfo, len(jobsByName))

	for _, job := range jobsByName {
		switch job.Type {
		case hybridwfv1alpha1.HybridWorkflowJobTypeK8s:
			if len(job.Outputs) > 0 {
				return fmt.Errorf("k8s job %q must declare outputs in jobSpec.outputs, not spec.jobs.outputs", job.Name)
			}
			if err := validateUniqueK8sInputNames(job); err != nil {
				return err
			}
			outputInfo, err := collectK8sOutputInfo(job)
			if err != nil {
				return err
			}
			outputsByJob[job.Name] = outputInfo

		case hybridwfv1alpha1.HybridWorkflowJobTypeSlurm:
			if job.Template != "" {
				return fmt.Errorf("slurm job %q must not define template", job.Name)
			}
			if err := validateSlurmInputShape(job); err != nil {
				return err
			}
			if err := validateUniqueSlurmOutputNames(job); err != nil {
				return err
			}
		}
	}

	for _, job := range jobsByName {
		for _, input := range job.Inputs {
			if input.From == "" {
				continue
			}

			sourceJob, outputName := splitSource(input.From)
			if _, exists := jobsByName[sourceJob]; !exists {
				return fmt.Errorf("job %q references unknown source job %q", job.Name, sourceJob)
			}
			sourceType := jobTypes[sourceJob]

			switch job.Type {
			case hybridwfv1alpha1.HybridWorkflowJobTypeK8s:
				if err := validateK8sInputReference(job, input, sourceJob, sourceType, outputName, outputsByJob[sourceJob]); err != nil {
					return err
				}
			case hybridwfv1alpha1.HybridWorkflowJobTypeSlurm:
				if err := validateSlurmInputReference(job, input, sourceJob, sourceType, outputName, outputsByJob[sourceJob]); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateUniqueK8sInputNames(job *hybridwfv1alpha1.HybridWorkflowJob) error {
	seen := make(map[string]struct{}, len(job.Inputs))
	for _, input := range job.Inputs {
		if input.Name == "" {
			continue
		}
		if _, exists := seen[input.Name]; exists {
			return fmt.Errorf("k8s job %q defines duplicate input name %q", job.Name, input.Name)
		}
		seen[input.Name] = struct{}{}
	}
	return nil
}

func validateSlurmInputShape(job *hybridwfv1alpha1.HybridWorkflowJob) error {
	fromInputs := 0
	s3Inputs := 0
	for _, input := range job.Inputs {
		if input.From != "" {
			fromInputs++
		}
		if input.S3Key != "" {
			s3Inputs++
		}
	}

	if fromInputs > 1 {
		return fmt.Errorf("slurm job %q may define at most one from input", job.Name)
	}
	if s3Inputs > 0 && fromInputs > 0 {
		return fmt.Errorf("slurm job %q cannot mix s3key inputs with from inputs", job.Name)
	}

	return nil
}

func validateUniqueSlurmOutputNames(job *hybridwfv1alpha1.HybridWorkflowJob) error {
	seen := make(map[string]struct{}, len(job.Outputs))
	for _, output := range job.Outputs {
		if _, exists := seen[output.Name]; exists {
			return fmt.Errorf("slurm job %q defines duplicate output %q", job.Name, output.Name)
		}
		seen[output.Name] = struct{}{}
	}
	return nil
}

func collectK8sOutputInfo(job *hybridwfv1alpha1.HybridWorkflowJob) (k8sOutputInfo, error) {
	info := k8sOutputInfo{
		artifacts:  map[string]struct{}{},
		parameters: map[string]struct{}{},
	}
	if job == nil || job.Type != hybridwfv1alpha1.HybridWorkflowJobTypeK8s || job.JobSpec == nil {
		return info, nil
	}

	jobSpec, err := decodeJSONObject(job.JobSpec)
	if err != nil {
		return info, fmt.Errorf("decode jobSpec for %q: %w", job.Name, err)
	}

	outputsValue, ok := jobSpec["outputs"]
	if !ok {
		return info, nil
	}

	outputs, ok := outputsValue.(map[string]any)
	if !ok {
		return info, fmt.Errorf("k8s job %q jobSpec.outputs must be an object", job.Name)
	}

	artifacts, err := collectNamedOutputEntries(outputs["artifacts"], "artifact", job.Name)
	if err != nil {
		return info, err
	}
	parameters, err := collectNamedOutputEntries(outputs["parameters"], "parameter", job.Name)
	if err != nil {
		return info, err
	}

	info.artifacts = artifacts
	info.parameters = parameters
	return info, nil
}

func collectNamedOutputEntries(section any, kind, jobName string) (map[string]struct{}, error) {
	names := map[string]struct{}{}
	if section == nil {
		return names, nil
	}

	var items []any
	switch typed := section.(type) {
	case []any:
		items = typed
	case []map[string]any:
		items = make([]any, 0, len(typed))
		for i := range typed {
			items = append(items, typed[i])
		}
	default:
		return nil, fmt.Errorf("k8s job %q jobSpec.outputs.%ss must be a list", jobName, kind)
	}

	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("k8s job %q jobSpec.outputs.%ss[%d] must be an object", jobName, kind, i)
		}
		name, ok := entry["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("k8s job %q jobSpec.outputs.%ss[%d] must define name", jobName, kind, i)
		}
		if _, exists := names[name]; exists {
			return nil, fmt.Errorf("k8s job %q defines duplicate %s output %q", jobName, kind, name)
		}
		names[name] = struct{}{}
	}

	return names, nil
}

func validateK8sInputReference(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	input hybridwfv1alpha1.HybridWorkflowInput,
	sourceJob string,
	sourceType hybridwfv1alpha1.HybridWorkflowJobType,
	outputName string,
	sourceOutputs k8sOutputInfo,
) error {
	inputName := input.Name
	if inputName == "" {
		inputName = input.From
	}

	if sourceType == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
		if input.Type != hybridwfv1alpha1.HybridWorkflowInputTypeArtifact {
			return fmt.Errorf("k8s job %q input %q must use type=artifact when referencing slurm job %q", job.Name, inputName, sourceJob)
		}
		if outputName != "" && outputName != "output-artifact" {
			return fmt.Errorf("k8s job %q input %q cannot reference slurm output %q from job %q; slurm jobs expose only output-artifact", job.Name, inputName, outputName, sourceJob)
		}
		return nil
	}

	if input.Type == hybridwfv1alpha1.HybridWorkflowInputTypeArtifact {
		if outputName == "" {
			if sourceOutputs.artifactCount() != 1 {
				return fmt.Errorf("k8s job %q input %q must reference an explicit artifact output from job %q because it does not expose exactly one artifact", job.Name, inputName, sourceJob)
			}
			return nil
		}
		if !sourceOutputs.hasArtifact(outputName) {
			return fmt.Errorf("k8s job %q input %q references unknown artifact output %q from job %q", job.Name, inputName, outputName, sourceJob)
		}
		return nil
	}

	if outputName != "" && outputName != "result" && !sourceOutputs.hasParameter(outputName) {
		return fmt.Errorf("k8s job %q input %q references unknown parameter output %q from job %q", job.Name, inputName, outputName, sourceJob)
	}

	return nil
}

func validateSlurmInputReference(
	job *hybridwfv1alpha1.HybridWorkflowJob,
	input hybridwfv1alpha1.HybridWorkflowInput,
	sourceJob string,
	sourceType hybridwfv1alpha1.HybridWorkflowJobType,
	outputName string,
	sourceOutputs k8sOutputInfo,
) error {
	if sourceType == hybridwfv1alpha1.HybridWorkflowJobTypeSlurm {
		if outputName != "" && outputName != "output-artifact" {
			return fmt.Errorf("slurm job %q input %q cannot reference slurm output %q from job %q; slurm jobs expose only output-artifact", job.Name, input.From, outputName, sourceJob)
		}
		return nil
	}

	if outputName == "" {
		if sourceOutputs.artifactCount() != 1 {
			return fmt.Errorf("slurm job %q input %q must reference an explicit artifact output from job %q because it does not expose exactly one artifact", job.Name, input.From, sourceJob)
		}
		return nil
	}

	if !sourceOutputs.hasArtifact(outputName) {
		return fmt.Errorf("slurm job %q input %q references unknown artifact output %q from job %q", job.Name, input.From, outputName, sourceJob)
	}

	return nil
}

func hasInlineContainerFields(spec map[string]any) bool {
	for _, key := range []string{"image", "command", "args"} {
		if _, ok := spec[key]; ok {
			return true
		}
	}
	return false
}

func hasOutput(outputs []hybridwfv1alpha1.HybridWorkflowOutput, name string) bool {
	for _, output := range outputs {
		if output.Name == name {
			return true
		}
	}
	return false
}

func resolveOutputName(
	sourceJob string,
	explicitOutputName string,
	jobsByName map[string]*hybridwfv1alpha1.HybridWorkflowJob,
	preferArtifact bool,
) string {
	if explicitOutputName != "" {
		return explicitOutputName
	}
	if preferArtifact {
		if artifactName := getSingleOutputArtifactName(jobsByName[sourceJob]); artifactName != "" {
			return artifactName
		}
	}
	return "result"
}

func getSingleOutputArtifactName(job *hybridwfv1alpha1.HybridWorkflowJob) string {
	if job == nil || job.Type != hybridwfv1alpha1.HybridWorkflowJobTypeK8s || job.JobSpec == nil {
		return ""
	}

	jobSpec, err := decodeJSONObject(job.JobSpec)
	if err != nil {
		return ""
	}

	outputs, ok := jobSpec["outputs"].(map[string]any)
	if !ok {
		return ""
	}
	artifacts, ok := outputs["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		return ""
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := artifact["name"].(string)
	return name
}

func decodeJSONObject(raw *apiextensionsv1.JSON) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("json object is nil")
	}
	if len(raw.Raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw.Raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeJSON(raw apiextensionsv1.JSON) (any, error) {
	if len(raw.Raw) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(raw.Raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mergeArguments(existing, incoming map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{"parameters", "artifacts"} {
		merged := map[string]map[string]any{}

		for _, item := range toNamedMapSlice(existing[key]) {
			merged[item["name"].(string)] = item
		}
		for _, item := range toNamedMapSlice(incoming[key]) {
			merged[item["name"].(string)] = item
		}

		if len(merged) == 0 {
			continue
		}

		names := make([]string, 0, len(merged))
		for name := range merged {
			names = append(names, name)
		}
		sort.Strings(names)

		items := make([]any, 0, len(names))
		for _, name := range names {
			items = append(items, merged[name])
		}
		result[key] = items
	}
	return result
}

func toNamedMapSlice(value any) []map[string]any {
	switch items := value.(type) {
	case nil:
		return nil
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if named, ok := item.(map[string]any); ok {
				if _, ok := named["name"].(string); ok {
					out = append(out, named)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func splitSource(source string) (string, string) {
	if source == "" {
		return "", ""
	}
	parts := strings.SplitN(source, ".", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func findDAGTemplate(workflow map[string]any) map[string]any {
	spec, ok := workflow["spec"].(map[string]any)
	if !ok {
		return nil
	}
	entrypoint, _ := spec["entrypoint"].(string)
	templates, _ := spec["templates"].([]any)

	if entrypoint != "" {
		for _, item := range templates {
			template, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if template["name"] == entrypoint {
				if _, ok := template["dag"].(map[string]any); ok {
					return template
				}
			}
		}
	}

	for _, item := range templates {
		template, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := template["dag"].(map[string]any); ok {
			return template
		}
	}

	return nil
}

func getTaskParameter(task map[string]any, name string) (string, bool) {
	args, ok := task["arguments"].(map[string]any)
	if !ok {
		return "", false
	}
	for _, item := range toNamedMapSlice(args["parameters"]) {
		if item["name"] == name {
			return fmt.Sprint(item["value"]), true
		}
	}
	return "", false
}

func ensureTaskParameter(task map[string]any, name string, value any) {
	args, ok := task["arguments"].(map[string]any)
	if !ok {
		args = map[string]any{}
		task["arguments"] = args
	}

	parameters := toNamedMapSlice(args["parameters"])
	for _, parameter := range parameters {
		if parameter["name"] == name {
			return
		}
	}

	parameters = append(parameters, map[string]any{
		"name":  name,
		"value": value,
	})
	sort.Slice(parameters, func(i, j int) bool {
		return fmt.Sprint(parameters[i]["name"]) < fmt.Sprint(parameters[j]["name"])
	})

	items := make([]any, 0, len(parameters))
	for _, parameter := range parameters {
		items = append(items, parameter)
	}
	args["parameters"] = items
}
