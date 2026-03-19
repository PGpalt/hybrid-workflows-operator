# HybridWorkflow CRD Schema Notes

This CRD is the schema-first version of the current Python compiler DSL in [workflow-gen.py](/home/p4nikasgr/Argo%20Workflows/Hybrid-Argo-Workflows/workflow-gen.py).

## What the CRD enforces directly

- `spec.jobs` is required and non-empty.
- Every job requires `name` and `type`.
- `type` is restricted to `k8s` or `slurm`.
- `k8s` jobs require `jobSpec`.
- `slurm` jobs require `command`.
- `slurm` jobs must not define `jobSpec`.
- `k8s` inputs must have `name`.
- `k8s` inputs may use `from` or `value`, but not `s3key` or `path`.
- `slurm` inputs may use `from` or `s3key`, but not `value`.
- `path` is only valid when paired with `s3key`.
- A `slurm` job may define at most one `s3key` input.
- `jobSpec` is preserved as schemaless Argo template content.

## What should still be validated in a webhook or controller

- Job names must be unique within one `HybridWorkflow`.
- Every `inputs[].from` reference must point to an existing job.
- The derived dependency graph must be acyclic.
- A `k8s` template override name must be unique after defaulting to `<job>-template`.
- Cleanup task names like `<job>-cleanup` must not collide with user job names.
- Bare artifact references such as `from: some-job` should be rejected when the source does not expose exactly one artifact and the fallback `result` path would be invalid for that usage.
- Any future scheduler-specific options should be validated in the controller, not forced into the CRD until the DSL stabilizes.

## Intended placement in a Kubebuilder project

When you scaffold the operator, copy this CRD into:

- `config/crd/bases/hybridwf.io_hybridworkflows.yaml`

Then mirror the same schema in Go markers under:

- `api/v1alpha1/hybridworkflow_types.go`
