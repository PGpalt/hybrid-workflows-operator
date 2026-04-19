#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
argo_namespace="${ARGO_NAMESPACE:-argo}"
ssh_dir="${SLURM_SSH_DIR:-/workspaces/.ssh/slurm}"
ssh_key_path="${SLURM_SSH_KEY_PATH:-${ssh_dir}/id_ed25519}"
ssh_secret_name="${SLURM_SSH_SECRET_NAME:-slurm-ssh-key}"
authorized_key_secret_name="${SLURM_AUTHORIZED_KEY_SECRET_NAME:-slurm-authorized-key}"
ssh_connect_info_manifest="${SLURM_SSH_CONNECT_INFO_MANIFEST:-${repo_root}/secret-templates/ssh-connect-info.yaml}"
slurm_service_manifest="${SLURM_SERVICE_MANIFEST:-${repo_root}/slurm/slurm-service.yaml}"
slurm_deployment_manifest="${SLURM_DEPLOYMENT_MANIFEST:-${repo_root}/slurm/slurm-deployment.yaml}"

require_command() {
  local command_name="$1"

  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "${command_name} is required but not installed or not on PATH." >&2
    exit 1
  fi
}

require_file() {
  local path="$1"

  if [[ ! -f "${path}" ]]; then
    echo "Required file not found: ${path}" >&2
    exit 1
  fi
}

for command_name in kubectl ssh-keygen chmod mkdir; do
  require_command "${command_name}"
done

require_file "${ssh_connect_info_manifest}"
require_file "${slurm_service_manifest}"
require_file "${slurm_deployment_manifest}"

mkdir -p "${ssh_dir}"
chmod 700 "${ssh_dir}"

if [[ ! -f "${ssh_key_path}" ]]; then
  ssh-keygen -q -t ed25519 -a 100 -N "" -f "${ssh_key_path}" -C "slurm-access"
else
  echo "SSH key already exists at ${ssh_key_path}; skipping generation."
fi

if [[ ! -f "${ssh_key_path}.pub" ]]; then
  echo "Missing public key: ${ssh_key_path}.pub" >&2
  exit 1
fi

kubectl -n "${argo_namespace}" create secret generic "${ssh_secret_name}" \
  --from-file=ssh-privatekey="${ssh_key_path}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${argo_namespace}" create secret generic "${authorized_key_secret_name}" \
  --from-file=authorized_keys="${ssh_key_path}.pub" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f "${ssh_connect_info_manifest}"
kubectl apply -f "${slurm_service_manifest}"
kubectl apply -f "${slurm_deployment_manifest}"

echo "AWS Slurm SSH secrets, connect info, service, and deployment have been applied."
