#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
workspace_parent="${SIBLING_WORKSPACE_DIR:-$(cd "${repo_root}/.." && pwd)}"
gitops_repo_dir="${GITOPS_REPO_DIR:-${workspace_parent}/hybrid-workflows-gitops}"
infra_repo_dir="${INFRA_REPO_DIR:-${workspace_parent}/hybrid-workflows-infra}"
gitops_repo_url="${GITOPS_REPO_URL:-https://github.com/PGpalt/hybrid-workflows-gitops.git}"
infra_repo_url="${INFRA_REPO_URL:-https://github.com/PGpalt/hybrid-workflows-infra.git}"
auto_clone_siblings="${AUTO_CLONE_SIBLINGS:-false}"

missing_repos=()

is_codespaces() {
  [[ -n "${CODESPACES:-}" ]]
}

has_git_metadata() {
  local repo_dir="$1"

  [[ -d "${repo_dir}/.git" || -f "${repo_dir}/.git" ]]
}

clone_repo_if_requested() {
  local repo_name="$1"
  local repo_dir="$2"
  local repo_url="$3"

  if [[ "${auto_clone_siblings}" != "true" ]] || is_codespaces; then
    return
  fi

  if ! command -v git >/dev/null 2>&1; then
    echo "git is required to auto-clone ${repo_name}." >&2
    return
  fi

  if [[ -e "${repo_dir}" ]] && ! has_git_metadata "${repo_dir}"; then
    echo "Skipping auto-clone for ${repo_name}: ${repo_dir} already exists and is not a git repo." >&2
    return
  fi

  mkdir -p "$(dirname "${repo_dir}")"
  echo "Cloning ${repo_name} into ${repo_dir}"
  if ! git clone "${repo_url}" "${repo_dir}"; then
    echo "Auto-clone failed for ${repo_name}. You can clone it manually with: git clone ${repo_url} ${repo_dir}" >&2
  fi
}

ensure_repo() {
  local repo_name="$1"
  local repo_dir="$2"
  local repo_url="$3"

  if has_git_metadata "${repo_dir}"; then
    echo "Found ${repo_name} at ${repo_dir}"
    return
  fi

  clone_repo_if_requested "${repo_name}" "${repo_dir}" "${repo_url}"

  if has_git_metadata "${repo_dir}"; then
    echo "Cloned ${repo_name} into ${repo_dir}"
    return
  fi

  missing_repos+=("${repo_name}|${repo_dir}|${repo_url}")
}

echo ""
if is_codespaces; then
  echo "Codespaces workspace ready for hybrid-workflows-operator."
  echo "Sibling repos are requested through customizations.codespaces.repositories."
else
  echo "Devcontainer workspace ready for hybrid-workflows-operator."
  echo "Parent workspace directory: ${workspace_parent}"
fi

ensure_repo "hybrid-workflows-gitops" "${gitops_repo_dir}" "${gitops_repo_url}"
ensure_repo "hybrid-workflows-infra" "${infra_repo_dir}" "${infra_repo_url}"

if [[ "${#missing_repos[@]}" -gt 0 ]]; then
  echo ""
  echo "Sibling repositories still need to be available inside the container:"
  for repo_entry in "${missing_repos[@]}"; do
    IFS='|' read -r repo_name repo_dir repo_url <<< "${repo_entry}"
    echo "- ${repo_name}: ${repo_dir}"
    echo "  clone with: git clone ${repo_url} ${repo_dir}"
  done
  if [[ "${auto_clone_siblings}" != "true" && ! is_codespaces ]]; then
    echo "Set AUTO_CLONE_SIBLINGS=true before rebuilding the container to clone them automatically on create."
  fi
fi

echo ""
echo "Toolchain is now baked into the devcontainer image."
if ! command -v kind >/dev/null 2>&1 || ! command -v kubebuilder >/dev/null 2>&1; then
  echo "Optional image tools skipped: kind, kubebuilder."
  echo "Set build.args.INSTALL_OPTIONAL_DEV_TOOLS=true in .devcontainer/devcontainer.json and rebuild if you need them."
fi
echo "AWS CLI, Terraform, kubectl, Minikube, Helm, kustomize, argocd, and jq are available in the image."
echo "Recommended next steps:"
echo "Configure AWS credentials, for example: aws configure --profile eks-dev"
echo "Then export AWS_PROFILE=eks-dev and AWS_REGION=eu-north-1 before running Terraform against EKS."
echo "Bootstrap the cluster from the sibling hybrid-workflows-infra repo, then run bash scripts/setup.sh."
