#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  SUDO="sudo"
fi

MACHINE="$(uname -m)"
case "${MACHINE}" in
  x86_64)
    ARCH="amd64"
    AWSCLI_ARCH="x86_64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    AWSCLI_ARCH="aarch64"
    ;;
  *)
    echo "Unsupported architecture ${MACHINE}" >&2
    exit 1
    ;;
esac

BASH_COMPLETIONS_DIR="/usr/share/bash-completion/completions"
INSTALL_OPTIONAL_DEV_TOOLS="${INSTALL_OPTIONAL_DEV_TOOLS:-false}"

${SUDO} apt-get update
${SUDO} apt-get install -y --no-install-recommends bash-completion ca-certificates curl jq unzip

if ! grep -q "bash_completion" "${HOME}/.bashrc" 2>/dev/null; then
  echo 'source /usr/share/bash-completion/bash_completion' >> "${HOME}/.bashrc"
fi

install_bin() {
  local name="$1"
  local url="$2"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "Installing ${name}..."
    curl -fsSL "${url}" -o "/tmp/${name}"
    ${SUDO} install -m 0755 "/tmp/${name}" "/usr/local/bin/${name}"
  fi
}

install_tar_gz_bin() {
  local name="$1"
  local url="$2"
  local archive="/tmp/${name}.tar.gz"
  local extract_dir="/tmp/${name}-extract"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "Installing ${name}..."
    rm -rf "${extract_dir}"
    mkdir -p "${extract_dir}"
    curl -fsSL "${url}" -o "${archive}"
    tar -xzf "${archive}" -C "${extract_dir}"
    ${SUDO} install -m 0755 "${extract_dir}/${name}" "/usr/local/bin/${name}"
  fi
}

install_zip_bin() {
  local name="$1"
  local url="$2"
  local archive="/tmp/${name}.zip"
  local extract_dir="/tmp/${name}-extract"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "Installing ${name}..."
    rm -rf "${extract_dir}"
    mkdir -p "${extract_dir}"
    curl -fsSL "${url}" -o "${archive}"
    unzip -oq "${archive}" -d "${extract_dir}"
    ${SUDO} install -m 0755 "${extract_dir}/${name}" "/usr/local/bin/${name}"
  fi
}

install_aws_cli() {
  local archive="/tmp/awscliv2.zip"
  local extract_dir="/tmp/awscli-extract"
  if ! command -v aws >/dev/null 2>&1; then
    echo "Installing aws..."
    rm -rf "${extract_dir}"
    mkdir -p "${extract_dir}"
    curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${AWSCLI_ARCH}.zip" -o "${archive}"
    unzip -oq "${archive}" -d "${extract_dir}"
    ${SUDO} "${extract_dir}/aws/install" --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update
  fi
}

if [[ "${INSTALL_OPTIONAL_DEV_TOOLS}" == "true" ]]; then
  if ! command -v kind >/dev/null 2>&1; then
    install_bin kind "https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-${ARCH}"
  fi

  if ! command -v kubebuilder >/dev/null 2>&1; then
    install_bin kubebuilder "https://go.kubebuilder.io/dl/latest/linux/${ARCH}"
  fi
fi

if ! command -v kubectl >/dev/null 2>&1; then
  KUBECTL_VERSION="$(curl -fsSL https://dl.k8s.io/release/stable.txt)"
  install_bin kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl"
fi

if ! command -v minikube >/dev/null 2>&1; then
  install_bin minikube "https://storage.googleapis.com/minikube/releases/latest/minikube-linux-${ARCH}"
fi

if ! command -v kustomize >/dev/null 2>&1; then
  KUSTOMIZE_VERSION="v5.7.1"
  KUSTOMIZE_URL="https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_${ARCH}.tar.gz"
  install_tar_gz_bin kustomize "${KUSTOMIZE_URL}"
fi

if ! command -v helm >/dev/null 2>&1; then
  HELM_VERSION="v3.18.6"
  HELM_ARCHIVE="/tmp/helm.tar.gz"
  HELM_EXTRACT_DIR="/tmp/helm-extract"
  curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${ARCH}.tar.gz" -o "${HELM_ARCHIVE}"
  rm -rf "${HELM_EXTRACT_DIR}"
  mkdir -p "${HELM_EXTRACT_DIR}"
  tar -xzf "${HELM_ARCHIVE}" -C "${HELM_EXTRACT_DIR}"
  ${SUDO} install -m 0755 "${HELM_EXTRACT_DIR}/linux-${ARCH}/helm" /usr/local/bin/helm
fi

if ! command -v terraform >/dev/null 2>&1; then
  TERRAFORM_VERSION="$(curl -fsSL https://checkpoint-api.hashicorp.com/v1/check/terraform | sed -n 's/.*"current_version":"\([^\"]*\)".*/\1/p')"
  if [[ -z "${TERRAFORM_VERSION}" ]]; then
    echo "Failed to determine the current Terraform version." >&2
    exit 1
  fi
  install_zip_bin terraform "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip"
fi

install_aws_cli

if ! command -v argocd >/dev/null 2>&1; then
  ARGOCD_VERSION="v3.1.1"
  install_bin argocd "https://github.com/argoproj/argo-cd/releases/download/${ARGOCD_VERSION}/argocd-linux-${ARCH}"
fi

for i in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if command -v kubectl >/dev/null 2>&1; then
  kubectl completion bash > "${BASH_COMPLETIONS_DIR}/kubectl" || true
fi
if command -v minikube >/dev/null 2>&1; then
  minikube completion bash > "${BASH_COMPLETIONS_DIR}/minikube" || true
fi
if command -v helm >/dev/null 2>&1; then
  helm completion bash > "${BASH_COMPLETIONS_DIR}/helm" || true
fi
if command -v docker >/dev/null 2>&1; then
  docker completion bash > "${BASH_COMPLETIONS_DIR}/docker" || true
fi

echo ""
echo "Codespaces tools installed for the operator repo."
if [[ "${INSTALL_OPTIONAL_DEV_TOOLS}" != "true" ]]; then
  echo "Optional tools skipped: kind, kubebuilder."
  echo "Set INSTALL_OPTIONAL_DEV_TOOLS=true before running post-install to include them."
fi
echo "AWS CLI and jq are installed for the EKS/Terraform workflow."
echo "Recommended next steps:"
echo "Configure AWS credentials, for example: aws configure --profile eks-dev"
echo "Then export AWS_PROFILE=eks-dev and AWS_REGION=eu-north-1 before running Terraform against EKS."
echo "Start Minikube, then use the operator and infra repo workflows to bootstrap the platform."
