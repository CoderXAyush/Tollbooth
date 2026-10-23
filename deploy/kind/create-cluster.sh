#!/usr/bin/env bash
# create-cluster.sh – creates a kind cluster for Tollbooth local development.
#
# Usage: ./deploy/kind/create-cluster.sh
#
# Prerequisites: kind, kubectl, docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-tollbooth}"

echo "==> Creating kind cluster '${CLUSTER_NAME}'..."
kind create cluster \
  --name "${CLUSTER_NAME}" \
  --config "${SCRIPT_DIR}/kind-config.yaml" \
  --wait 60s

echo "==> Installing NGINX Ingress Controller..."
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml

echo "==> Waiting for ingress controller to be ready..."
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s

echo "==> Cluster '${CLUSTER_NAME}' is ready!"
echo "    kubectl cluster-info --context kind-${CLUSTER_NAME}"
