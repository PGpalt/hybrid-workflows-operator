# syntax=docker/dockerfile:1.7

ARG VARIANT=1.25-bookworm
FROM mcr.microsoft.com/devcontainers/go:${VARIANT}

USER root

ARG INSTALL_OPTIONAL_DEV_TOOLS=false
ARG KUBECTL_VERSION=stable
ARG MINIKUBE_VERSION=latest
ARG TERRAFORM_VERSION=latest
ARG KUSTOMIZE_VERSION=v5.7.1
ARG HELM_VERSION=v3.18.6
ARG ARGOCD_VERSION=v3.1.1

ENV DEBIAN_FRONTEND=noninteractive

COPY .devcontainer/install-tools.sh /usr/local/share/devcontainer/install-tools.sh

RUN chmod 0755 /usr/local/share/devcontainer/install-tools.sh \
    && /usr/local/share/devcontainer/install-tools.sh \
    && rm -f /usr/local/share/devcontainer/install-tools.sh

USER vscode
