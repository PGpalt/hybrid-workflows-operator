#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

if [[ ! -f sample_R1.fastq.gz ]]; then
  wget ftp://ftp.sra.ebi.ac.uk/vol1/fastq/SRR513/SRR513053/SRR513053_1.fastq.gz -O sample_R1.fastq.gz
else
  echo "sample_R1.fastq.gz already exists, skipping download"
fi

if [[ ! -f sample_R2.fastq.gz ]]; then
  wget ftp://ftp.sra.ebi.ac.uk/vol1/fastq/SRR513/SRR513053/SRR513053_2.fastq.gz -O sample_R2.fastq.gz
else
  echo "sample_R2.fastq.gz already exists, skipping download"
fi
