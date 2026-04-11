#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"
argocd_namespace="${ARGOCD_NAMESPACE:-argocd}"
argo_namespace="${ARGO_NAMESPACE:-argo}"
katib_namespace="${KATIB_NAMESPACE:-kubeflow}"
slurm_container_name="${SLURM_CONTAINER_NAME:-slurm-container}"
minio_bucket="${MINIO_BUCKET:-my-bucket}"
minio_secret_name="${MINIO_SECRET_NAME:-my-minio-cred}"
minio_access_key="${MINIO_ACCESS_KEY:-}"
minio_secret_key="${MINIO_SECRET_KEY:-}"
minio_client_image="${MINIO_CLIENT_IMAGE:-minio/mc}"
ssh_dir="${HOME}/.ssh/slurm"
ssh_key_path="${ssh_dir}/id_ed25519"

require_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "${command_name} is required but not installed or not on PATH." >&2
    exit 1
  fi
}

is_container_runtime() {
  [ -f "/.dockerenv" ] || [ -n "${CODESPACES:-}" ] || [ -n "${REMOTE_CONTAINERS:-}" ]
}

wait_for_service() {
  local namespace="$1"
  local service="$2"
  local timeout_seconds="${3:-600}"
  local deadline=$((SECONDS + timeout_seconds))

  until kubectl get svc "${service}" -n "${namespace}" >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "Timed out waiting for service ${namespace}/${service}." >&2
      exit 1
    fi
    sleep 5
  done
}

get_nodeport() {
  local namespace="$1"
  local service="$2"
  local port="$3"
  local nodeport

  nodeport="$(kubectl get svc "${service}" -n "${namespace}" -o jsonpath="{.spec.ports[?(@.port==${port})].nodePort}")"
  if [[ -z "${nodeport}" ]]; then
    echo "Failed to resolve NodePort for ${namespace}/${service} port ${port}." >&2
    exit 1
  fi
  echo "${nodeport}"
}

wait_for_secret() {
  local namespace="$1"
  local secret_name="$2"
  local timeout_seconds="${3:-600}"
  local deadline=$((SECONDS + timeout_seconds))

  until kubectl get secret "${secret_name}" -n "${namespace}" >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "Timed out waiting for secret ${namespace}/${secret_name}." >&2
      exit 1
    fi
    sleep 5
  done
}

get_secret_value() {
  local namespace="$1"
  local secret_name="$2"
  local key="$3"
  local encoded_value=""

  encoded_value="$(kubectl get secret "${secret_name}" -n "${namespace}" -o jsonpath="{.data.${key}}" 2>/dev/null || true)"
  if [[ -z "${encoded_value}" ]]; then
    echo "Failed to resolve ${key} from secret ${namespace}/${secret_name}." >&2
    exit 1
  fi

  printf '%s' "${encoded_value}" | base64 --decode
}

load_minio_credentials() {
  if [[ -n "${minio_access_key}" && -n "${minio_secret_key}" ]]; then
    return
  fi

  if [[ -n "${minio_access_key}" || -n "${minio_secret_key}" ]]; then
    echo "Set both MINIO_ACCESS_KEY and MINIO_SECRET_KEY together, or leave both unset." >&2
    exit 1
  fi

  wait_for_secret "${argo_namespace}" "${minio_secret_name}"
  minio_access_key="$(get_secret_value "${argo_namespace}" "${minio_secret_name}" accesskey)"
  minio_secret_key="$(get_secret_value "${argo_namespace}" "${minio_secret_name}" secretkey)"
}

run_minio_client() {
  local minio_host="$1"
  local minio_port="$2"
  shift 2

  docker run --rm --network host \
    -v "${repo_root}/examples/Datasets:/datasets:ro" \
    -e "MC_HOST_local=http://${minio_access_key}:${minio_secret_key}@${minio_host}:${minio_port}" \
    "${minio_client_image}" "$@"
}

upload_example_datasets() {
  local minio_host="$1"
  local minio_port="$2"
  local ready="false"

  for _ in $(seq 1 60); do
    if run_minio_client "${minio_host}" "${minio_port}" ls "local/${minio_bucket}" >/dev/null 2>&1; then
      ready="true"
      break
    fi
    sleep 5
  done

  if [[ "${ready}" != "true" ]]; then
    echo "Timed out waiting for MinIO bucket ${minio_bucket} to become reachable." >&2
    exit 1
  fi

  run_minio_client "${minio_host}" "${minio_port}" mb --ignore-existing "local/${minio_bucket}" >/dev/null
  run_minio_client "${minio_host}" "${minio_port}" mirror --overwrite /datasets/GenomicData "local/${minio_bucket}/GenomicData" >/dev/null
  run_minio_client "${minio_host}" "${minio_port}" mirror --overwrite /datasets/GenomicDataAris "local/${minio_bucket}/GenomicDataAris" >/dev/null
  run_minio_client "${minio_host}" "${minio_port}" mirror --overwrite /datasets/Mnist-Dataset "local/${minio_bucket}/Mnist-Dataset" >/dev/null
}

require_running_minikube() {
  local status

  minikube update-context >/dev/null 2>&1 || true
  status="$(minikube status --format='{{.Host}} {{.Kubelet}} {{.APIServer}}' 2>/dev/null || true)"

  if [[ "${status}" != "Running Running Running" ]]; then
    echo "Minikube must already be running before setup." >&2
    echo "Start it first, for example:" >&2
    echo "  minikube start --driver=docker --cpus=4 --memory=4096 --disk-size=10g" >&2
    exit 1
  fi
}

for command_name in base64 minikube kubectl docker ssh-keygen; do
  require_command "${command_name}"
done

require_running_minikube
load_minio_credentials

bash "${repo_root}/slurm/install-dummy-slurm-cluster.sh"

wait_for_service "${argocd_namespace}" argocd-server
wait_for_service "${argo_namespace}" argo-server
wait_for_service "${argo_namespace}" minio
wait_for_service "${argo_namespace}" minio-console
wait_for_service "${katib_namespace}" katib-ui

resolved_argocd_nodeport="$(get_nodeport "${argocd_namespace}" argocd-server 443)"
resolved_argo_workflows_nodeport="$(get_nodeport "${argo_namespace}" argo-server 2746)"
resolved_minio_api_nodeport="$(get_nodeport "${argo_namespace}" minio 9000)"
resolved_minio_console_nodeport="$(get_nodeport "${argo_namespace}" minio-console 9001)"
resolved_katib_nodeport="$(get_nodeport "${katib_namespace}" katib-ui 80)"

upload_example_datasets "$(minikube ip)" "${resolved_minio_api_nodeport}"

mkdir -p "${ssh_dir}"
chmod 700 "${ssh_dir}"

if [ ! -f "${ssh_key_path}" ]; then
  ssh-keygen -q -t ed25519 -a 100 -N "" -f "${ssh_key_path}" -C "slurm-access"
else
  echo "SSH key already exists at ${ssh_key_path}; skipping generation."
fi

public_key="$(cat "${ssh_key_path}.pub")"
docker exec "${slurm_container_name}" /bin/sh -lc "umask 077
mkdir -p /root/.ssh
touch /root/.ssh/authorized_keys
chmod 700 /root/.ssh
chmod 600 /root/.ssh/authorized_keys
grep -qxF '${public_key}' /root/.ssh/authorized_keys || printf '%s\n' '${public_key}' >> /root/.ssh/authorized_keys
echo 'Added public key to /root/.ssh/authorized_keys'"

kubectl -n "${argo_namespace}" create secret generic slurm-ssh-key \
  --from-file=ssh-privatekey=${ssh_key_path} \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${argo_namespace}" apply -f "${repo_root}/secret-templates/ssh-creds-example.yaml"

if [[ -n "${CODESPACES:-}" ]]; then
  echo "Running in GitHub Codespaces."
  echo "Next step: run bash scripts/port-forward-uis.sh in a separate terminal."
  echo "Then open the forwarded ports from the PORTS tab."
  echo "Expected forwarded ports: ArgoCD 8080, Argo Workflows 2746, MinIO Console 9001, Katib 8081."
elif is_container_runtime; then
  echo "Running in a devcontainer."
  echo "Next step: run bash scripts/port-forward-uis.sh in a separate terminal."
  echo "Then open these localhost URLs from your browser:"
  echo "ArgoCD Server: http://127.0.0.1:8080"
  echo "Argo Workflows Server: http://127.0.0.1:2746"
  echo "MinIO Console: http://127.0.0.1:9001"
  echo "Katib Frontend: http://127.0.0.1:8081/katib/"
else
  echo "Running on the local host. Open these services using the Minikube IP and NodePorts below."
  echo "ArgoCD Server: https://$(minikube ip):${resolved_argocd_nodeport}"
  echo "Argo Workflows Server: https://$(minikube ip):${resolved_argo_workflows_nodeport}"
  echo "MinIO Console: https://$(minikube ip):${resolved_minio_console_nodeport}"
  echo "Katib Frontend: https://$(minikube ip):${resolved_katib_nodeport}/katib/"
fi

echo "ArgoCD Username: admin"
echo "ArgoCD Password: managed by Terraform in hybrid-workflows-infra"
echo "MinIO credentials: read from ${argo_namespace}/${minio_secret_name}"
echo "Datasets uploaded to MinIO bucket: ${minio_bucket}"
echo "Setup complete."
echo "SSH private key: ${ssh_key_path}"
