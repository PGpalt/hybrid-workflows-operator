#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

wget ftp://ftp.sra.ebi.ac.uk/vol1/fastq/SRR513/SRR513053/SRR513053_1.fastq.gz -O sample_R1.fastq.gz
wget ftp://ftp.sra.ebi.ac.uk/vol1/fastq/SRR513/SRR513053/SRR513053_2.fastq.gz -O sample_R2.fastq.gz
