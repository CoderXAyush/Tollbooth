#!/usr/bin/env bash
# up.sh – Idempotently creates the kind cluster and installs ingress-nginx
#
# Usage: ./deploy/kind/up.sh
#
# Prerequisites: kind, kubectl, docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-tollbooth}"
INGRESS_NGINX_VERSION="${INGRESS_NGINX_VERSION:-controller-v1.11.3}"

echo "==> Checking if kind cluster '${CLUSTER_NAME}' already exists..."
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  echo "    Cluster '${CLUSTER_NAME}' already exists. Skipping creation."
else
  echo "==> Creating kind cluster '${CLUSTER_NAME}'..."
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --config "${SCRIPT_DIR}/kind-config.yaml" \
    --wait 120s
fi

echo "==> Ensuring nodes are Ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=60s

echo "==> Installing NGINX Ingress Controller (${INGRESS_NGINX_VERSION})..."
kubectl apply -f "https://raw.githubusercontent.com/kubernetes/ingress-nginx/${INGRESS_NGINX_VERSION}/deploy/static/provider/kind/deploy.yaml"

echo "==> Waiting for ingress controller to be ready..."
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=180s

echo "==> Kind cluster '${CLUSTER_NAME}' is up and ingress-ready!"
echo "    Context: kind-${CLUSTER_NAME}"
