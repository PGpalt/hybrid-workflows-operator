#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
container_name="${SLURM_CONTAINER_NAME:-slurm-container}"
container_hostname="${SLURM_CONTAINER_HOSTNAME:-slurmctl}"

bash "$repo_root/examples/Datasets/GenomicData/download-sample-fastq.sh"

cd "$script_dir"

docker build -t slurm-container:latest .

docker run -dit \
  --name "$container_name" \
  -h "$container_hostname" \
  -p 2220:22 \
  --cap-add sys_admin \
  slurm-container:latest

echo "Started container: $container_name"
