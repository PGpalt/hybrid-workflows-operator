#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
container_name="${SLURM_CONTAINER_NAME:-slurm-container}"
container_hostname="${SLURM_CONTAINER_HOSTNAME:-slurmctl}"
image_name="${SLURM_IMAGE_NAME:-slurm-container:latest}"
rebuild_image="${SLURM_REBUILD_IMAGE:-false}"

bash "$repo_root/examples/Datasets/GenomicData/download-sample-fastq.sh"

cd "$script_dir"

if [[ "${rebuild_image}" == "true" ]] || ! docker image inspect "${image_name}" >/dev/null 2>&1; then
  docker build -t "${image_name}" .
else
  echo "Using existing image: ${image_name}"
fi

if docker container inspect "${container_name}" >/dev/null 2>&1; then
  container_state="$(docker inspect -f '{{.State.Status}}' "${container_name}")"
  if [[ "${container_state}" == "running" ]]; then
    echo "Container already running: ${container_name}"
  else
    docker start "${container_name}" >/dev/null
    echo "Started existing container: ${container_name}"
  fi
else
  docker run -dit \
    --name "${container_name}" \
    -h "${container_hostname}" \
    -p 2220:22 \
    --cap-add sys_admin \
    "${image_name}" >/dev/null
  echo "Started new container: ${container_name}"
fi

echo "Dummy Slurm container is ready: ${container_name}"
