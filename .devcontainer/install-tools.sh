#!/usr/bin/env bash
set -euo pipefail

kubectl_version="${KUBECTL_VERSION:-stable}"
minikube_version="${MINIKUBE_VERSION:-latest}"
terraform_version="${TERRAFORM_VERSION:-latest}"
kustomize_version="${KUSTOMIZE_VERSION:-v5.7.1}"
helm_version="${HELM_VERSION:-v3.18.6}"
argocd_version="${ARGOCD_VERSION:-v3.1.1}"
install_optional_dev_tools="${INSTALL_OPTIONAL_DEV_TOOLS:-false}"

bash_completions_dir="/usr/share/bash-completion/completions"

resolve_arch() {
  local machine

  machine="$(dpkg --print-architecture)"
  case "${machine}" in
    amd64)
      arch="amd64"
      awscli_arch="x86_64"
      ;;
    arm64)
      arch="arm64"
      awscli_arch="aarch64"
      ;;
    *)
      echo "Unsupported architecture ${machine}" >&2
      exit 1
      ;;
  esac
}

install_bin() {
  local name="$1"
  local url="$2"

  echo "Installing ${name}..."
  curl -fsSL "${url}" -o "/tmp/${name}"
  install -m 0755 "/tmp/${name}" "/usr/local/bin/${name}"
}

install_tar_gz_bin() {
  local name="$1"
  local url="$2"
  local archive="/tmp/${name}.tar.gz"
  local extract_dir="/tmp/${name}-extract"

  echo "Installing ${name}..."
  rm -rf "${extract_dir}"
  mkdir -p "${extract_dir}"
  curl -fsSL "${url}" -o "${archive}"
  tar -xzf "${archive}" -C "${extract_dir}"
  install -m 0755 "${extract_dir}/${name}" "/usr/local/bin/${name}"
}

install_zip_bin() {
  local name="$1"
  local url="$2"
  local archive="/tmp/${name}.zip"
  local extract_dir="/tmp/${name}-extract"

  echo "Installing ${name}..."
  rm -rf "${extract_dir}"
  mkdir -p "${extract_dir}"
  curl -fsSL "${url}" -o "${archive}"
  unzip -oq "${archive}" -d "${extract_dir}"
  install -m 0755 "${extract_dir}/${name}" "/usr/local/bin/${name}"
}

install_aws_cli() {
  local archive="/tmp/awscliv2.zip"
  local extract_dir="/tmp/awscli-extract"

  echo "Installing aws..."
  rm -rf "${extract_dir}"
  mkdir -p "${extract_dir}"
  curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${awscli_arch}.zip" -o "${archive}"
  unzip -oq "${archive}" -d "${extract_dir}"
  "${extract_dir}/aws/install" --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update
}

resolve_kubectl_version() {
  if [[ "${kubectl_version}" == "stable" ]]; then
    kubectl_version="$(curl -fsSL https://dl.k8s.io/release/stable.txt)"
  fi
}

resolve_terraform_version() {
  if [[ "${terraform_version}" == "latest" ]]; then
    terraform_version="$(curl -fsSL https://checkpoint-api.hashicorp.com/v1/check/terraform | sed -n 's/.*"current_version":"\([^"]*\)".*/\1/p')"
  fi

  if [[ -z "${terraform_version}" ]]; then
    echo "Failed to determine the Terraform version." >&2
    exit 1
  fi
}

install_optional_tools() {
  if [[ "${install_optional_dev_tools}" != "true" ]]; then
    return
  fi

  install_bin kind "https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-${arch}"
  install_bin kubebuilder "https://go.kubebuilder.io/dl/latest/linux/${arch}"
}

configure_bash_completion() {
  if ! grep -q "bash_completion" /etc/bash.bashrc 2>/dev/null; then
    echo 'source /usr/share/bash-completion/bash_completion' >> /etc/bash.bashrc
  fi

  kubectl completion bash > "${bash_completions_dir}/kubectl"
  minikube completion bash > "${bash_completions_dir}/minikube"
  helm completion bash > "${bash_completions_dir}/helm"
}

cleanup() {
  rm -rf /tmp/aws /tmp/awscli-extract /tmp/helm-extract /tmp/kustomize-extract
  rm -f /tmp/argocd /tmp/awscliv2.zip /tmp/helm.tar.gz /tmp/kubectl /tmp/kustomize.tar.gz /tmp/minikube /tmp/terraform.zip
  apt-get clean
  rm -rf /var/lib/apt/lists/*
}

main() {
  resolve_arch

  apt-get update
  apt-get install -y --no-install-recommends bash-completion ca-certificates curl jq unzip

  resolve_kubectl_version
  resolve_terraform_version

  install_optional_tools

  install_bin kubectl "https://dl.k8s.io/release/${kubectl_version}/bin/linux/${arch}/kubectl"

  if [[ "${minikube_version}" == "latest" ]]; then
    install_bin minikube "https://storage.googleapis.com/minikube/releases/latest/minikube-linux-${arch}"
  else
    install_bin minikube "https://storage.googleapis.com/minikube/releases/${minikube_version}/minikube-linux-${arch}"
  fi

  install_tar_gz_bin kustomize "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${kustomize_version}/kustomize_${kustomize_version}_linux_${arch}.tar.gz"

  curl -fsSL "https://get.helm.sh/helm-${helm_version}-linux-${arch}.tar.gz" -o /tmp/helm.tar.gz
  rm -rf /tmp/helm-extract
  mkdir -p /tmp/helm-extract
  tar -xzf /tmp/helm.tar.gz -C /tmp/helm-extract
  install -m 0755 "/tmp/helm-extract/linux-${arch}/helm" /usr/local/bin/helm

  install_zip_bin terraform "https://releases.hashicorp.com/terraform/${terraform_version}/terraform_${terraform_version}_linux_${arch}.zip"
  install_aws_cli
  install_bin argocd "https://github.com/argoproj/argo-cd/releases/download/${argocd_version}/argocd-linux-${arch}"

  configure_bash_completion
  cleanup
}

main "$@"
