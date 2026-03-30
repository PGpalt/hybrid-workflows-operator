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
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture ${MACHINE}" >&2
    exit 1
    ;;
esac

BASH_COMPLETIONS_DIR="/usr/share/bash-completion/completions"

${SUDO} apt-get update
${SUDO} apt-get install -y --no-install-recommends bash-completion ca-certificates curl unzip

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

if ! command -v kind >/dev/null 2>&1; then
  install_bin kind "https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-${ARCH}"
fi

if ! command -v kubebuilder >/dev/null 2>&1; then
  install_bin kubebuilder "https://go.kubebuilder.io/dl/latest/linux/${ARCH}"
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

if ! docker network inspect kind >/dev/null 2>&1; then
  docker network create kind >/dev/null 2>&1 || true
fi

if command -v kind >/dev/null 2>&1; then
  kind completion bash > "${BASH_COMPLETIONS_DIR}/kind" || true
fi
if command -v kubebuilder >/dev/null 2>&1; then
  kubebuilder completion bash > "${BASH_COMPLETIONS_DIR}/kubebuilder" || true
fi
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
echo "Recommended next steps:"
echo "  1. git clone https://github.com/PGpalt/hybrid-workflows-gitops.git ../hybrid-workflows-gitops"
echo "  2. minikube start --driver=docker --cpus=4 --memory=8192 --disk-size=40g"
echo "  3. kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -"
echo "  4. kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml"
echo "  5. kubectl apply -f ../hybrid-workflows-gitops/bootstrap/root-application.yaml"
