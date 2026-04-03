#!/usr/bin/env bash

set -euo pipefail

target_host="${1:-$(minikube ip)}"
state_dir="${TMPDIR:-/tmp}/hybrid-workflows-nodeport-relay"
pid_file="${state_dir}/pids"
argocd_nodeport="${ARGOCD_NODEPORT:-32002}"
argo_workflows_nodeport="${ARGO_WORKFLOWS_NODEPORT:-32746}"
minio_api_nodeport="${MINIO_API_NODEPORT:-32000}"
minio_console_nodeport="${MINIO_CONSOLE_NODEPORT:-32001}"
katib_nodeport="${KATIB_NODEPORT:-30080}"

if ! command -v socat >/dev/null 2>&1; then
  echo "socat is required but not installed or not on PATH." >&2
  exit 1
fi

mkdir -p "${state_dir}"

if [ -f "${pid_file}" ]; then
  while read -r existing_pid; do
    if [ -n "${existing_pid}" ] && kill -0 "${existing_pid}" >/dev/null 2>&1; then
      kill "${existing_pid}" >/dev/null 2>&1 || true
    fi
  done < "${pid_file}"
  rm -f "${pid_file}"
fi

relay_ports=(
  "${minio_api_nodeport} MinIOAPI"
  "${minio_console_nodeport} MinIOConsole"
  "${argocd_nodeport} ArgoCD"
  "${argo_workflows_nodeport} ArgoWorkflows"
  "${katib_nodeport} Katib"
)

pids=()

cleanup() {
  local pid
  for pid in "${pids[@]:-}"; do
    if kill -0 "${pid}" >/dev/null 2>&1; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
  rm -f "${pid_file}"
}

trap cleanup EXIT INT TERM

for relay in "${relay_ports[@]}"; do
  read -r listen_port label <<<"${relay}"
  socat "TCP-LISTEN:${listen_port},bind=0.0.0.0,reuseaddr,fork" "TCP:${target_host}:${listen_port}" &
  pids+=("$!")
done

sleep 1

: > "${pid_file}"
for i in "${!relay_ports[@]}"; do
  read -r listen_port label <<<"${relay_ports[$i]}"
  if ! kill -0 "${pids[$i]}" >/dev/null 2>&1; then
    echo "Failed to start relay for ${label} on port ${listen_port}." >&2
    exit 1
  fi
  echo "${pids[$i]}" >> "${pid_file}"
  echo "${label} relay: 0.0.0.0:${listen_port} -> ${target_host}:${listen_port}"
done

echo
echo "NodePort relays are running. Keep this process alive to keep the relays active."
wait
