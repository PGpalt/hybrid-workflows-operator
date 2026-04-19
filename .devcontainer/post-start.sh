#!/usr/bin/env bash
set -euo pipefail

bash_completions_dir="/usr/share/bash-completion/completions"

if ! command -v docker >/dev/null 2>&1; then
  exit 0
fi

for _ in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then
    if command -v sudo >/dev/null 2>&1 && [[ -d "${bash_completions_dir}" ]] && [[ ! -f "${bash_completions_dir}/docker" ]]; then
      docker completion bash | sudo tee "${bash_completions_dir}/docker" >/dev/null || true
    fi
    echo "Docker in Docker is ready."
    exit 0
  fi
  sleep 1
done

echo "Docker is still starting. If Docker or Minikube commands fail, wait a few seconds and retry." >&2
