# Hybrid Workflows Operator

Sibling repos: hybrid-workflows-gitops and hybrid-workflows-infra

## Setup Guide

### Local host Ubuntu

Requirements:

- Docker installed and working
- Minikube installed with the Docker driver
- `kubectl` installed
- `ssh-keygen` available
- cluster bootstrap already completed from the sibling `hybrid-workflows-infra` repo

Then run:

```bash
bash scripts/setup.sh
```

### DevContainer or GitHub Codespaces with VS Code for Minikube overlay

Bootstrap the cluster first from the sibling `hybrid-workflows-infra` repo,
then run:

```bash
bash scripts/setup.sh
bash scripts/port-forward-uis.sh
```

Use localhost or the Codespaces PORTS tab for the forwarded UIs.

The image includes `aws`, `terraform`, `kubectl`, `minikube`, `helm`, `kustomize`,
`argocd`, and `jq`, so you can also drive the EKS demo workflow from
Codespaces or a local devcontainer. The simplest AWS setup inside the container
is:

```bash
aws configure --profile eks-dev
export AWS_PROFILE=eks-dev
export AWS_REGION=eu-north-1
```

After that, you can use the sibling infra repo Terraform environments against
EKS from the same Codespace.

Codespaces note:

- `.devcontainer/devcontainer.json` requests access to `PGpalt/hybrid-workflows-gitops` and `PGpalt/hybrid-workflows-infra`
- if you fork this project or use different repo names, update those repository entries before creating a new Codespace

Local devcontainer note:

- the devcontainer mounts the parent directory of this repo into `/workspaces`, so sibling repos are available side by side inside the container
- if `hybrid-workflows-gitops` and `hybrid-workflows-infra` already exist next to this repo on your host, they will appear automatically in the container
- if they do not exist yet, `post-install.sh` prints the exact `git clone` commands to create them
- if you prefer automatic local cloning, set `AUTO_CLONE_SIBLINGS=true` in `.devcontainer/devcontainer.json` before rebuilding the container
- if you want `kind` and `kubebuilder` baked into the image too, set `build.args.INSTALL_OPTIONAL_DEV_TOOLS=true` and rebuild

## Forking This Stack

If you fork the operator and want the full three-repo workflow to work with your
own repos:

- update `.devcontainer/devcontainer.json` if your GitOps or infra repos live under a different owner or name
- set the operator repo secret `GITOPS_REPO_TOKEN` if you want the release workflow to promote manifests into your GitOps repo
- optionally set the operator repo variable `GITOPS_REPO` to `owner/repo`; if unset, the release workflow defaults to `${GITHUB_REPOSITORY_OWNER}/hybrid-workflows-gitops`
- optionally set the operator repo variable `GITOPS_REPO_BRANCH`; if unset, the release workflow pushes to `main`
- optionally set the operator repo variable `GITOPS_OPERATOR_OVERLAY_FILE`; if unset, the release workflow updates all three operator overlays: `minikube`, `eks-dev`, and `eks-prod`
- update the sibling infra repo configuration if your GitOps repo URL is not `https://github.com/<owner>/hybrid-workflows-gitops.git`
- update the sibling GitOps repo if you want committed `repoURL` fields or image names to point at your own forked repos and registries

## What `setup.sh` Does Now

- installs or reuses the dummy Slurm container
- reads the existing MinIO credentials from the cluster and uploads the example datasets to `my-bucket`
- creates or reuses the SSH key and Kubernetes secrets used by the Slurm integration
- prints the service URLs for Argo CD, Argo Workflows, MinIO Console, and Katib

## What `setup.sh` Expects

- Argo CD, the root Application, and MinIO bootstrap credentials are already managed by `hybrid-workflows-infra`
- the Argo CD admin password is already managed by `hybrid-workflows-infra`
