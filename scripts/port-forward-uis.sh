#!/usr/bin/env bash

set -euo pipefail

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required but not installed or not on PATH." >&2
  exit 1
fi

services=(
  "argocd argocd-server 8080:443 ArgoCD"
  "argo argo-server 2746:2746 ArgoWorkflows"
  "argo minio 9000:9000 MinIOAPI"
  "argo minio-console 9001:9001 MinIOConsole"
  "kubeflow katib-ui 8081:80 KatibUI"
)

log_dir="$(mktemp -d)"
pids=()

cleanup() {
  local pid
  for pid in "${pids[@]:-}"; do
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
    fi
  done
  rm -rf "$log_dir"
}

trap cleanup EXIT INT TERM

for entry in "${services[@]}"; do
  read -r namespace service mapping label <<<"$entry"

  if ! kubectl get svc "$service" -n "$namespace" >/dev/null 2>&1; then
    echo "Missing service $namespace/$service. Is the app installed and synced?" >&2
    exit 1
  fi

  log_file="$log_dir/${namespace}-${service}.log"
  kubectl port-forward -n "$namespace" "svc/$service" "$mapping" >"$log_file" 2>&1 &
  pids+=("$!")
done

sleep 2

for i in "${!services[@]}"; do
  read -r namespace service mapping label <<<"${services[$i]}"
  local_port="${mapping%%:*}"

  if kill -0 "${pids[$i]}" >/dev/null 2>&1; then
    echo "$label: http://127.0.0.1:$local_port"
  else
    echo "Failed to port-forward $namespace/$service" >&2
    cat "$log_dir/${namespace}-${service}.log" >&2
    exit 1
  fi
done

echo
echo "Port-forwards are running. Keep this shell open. Press Ctrl-C to stop all of them."
wait
