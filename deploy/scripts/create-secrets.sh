#!/usr/bin/env bash
# Idempotently generate required Kubernetes Secrets without printing values.

set -euo pipefail

NAMESPACE="${1:-${NAMESPACE:-tollbooth}}"

rand_str() {
  openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 24
}

ensure_namespace() {
  kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1 ||
    kubectl create namespace "${NAMESPACE}" >/dev/null
}

secret_exists() {
  kubectl get secret "$1" -n "${NAMESPACE}" >/dev/null 2>&1
}

ensure_namespace
echo "==> Ensuring secrets in namespace '${NAMESPACE}'..."

if secret_exists mariadb-credentials; then
  echo "  [✓] mariadb-credentials exists"
else
  kubectl create secret generic mariadb-credentials -n "${NAMESPACE}" \
    --from-literal=MYSQL_ROOT_PASSWORD="$(rand_str)" \
    --from-literal=MYSQL_USER="wordpress" \
    --from-literal=MYSQL_PASSWORD="$(rand_str)" \
    --from-literal=MYSQL_DATABASE="wordpress" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] mariadb-credentials created"
fi

if secret_exists postgres-credentials; then
  echo "  [✓] postgres-credentials exists"
else
  kubectl create secret generic postgres-credentials -n "${NAMESPACE}" \
    --from-literal=POSTGRES_USER="tollbooth" \
    --from-literal=POSTGRES_PASSWORD="$(rand_str)" \
    --from-literal=POSTGRES_DB="tollbooth" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] postgres-credentials created"
fi

if secret_exists wordpress-admin-credentials; then
  echo "  [✓] wordpress-admin-credentials exists"
else
  kubectl create secret generic wordpress-admin-credentials -n "${NAMESPACE}" \
    --from-literal=ADMIN_USER="admin" \
    --from-literal=ADMIN_PASSWORD="$(rand_str)" \
    --from-literal=ADMIN_EMAIL="admin@example.com" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] wordpress-admin-credentials created"
fi

if secret_exists woocommerce-api-keys; then
  echo "  [✓] woocommerce-api-keys exists"
else
  kubectl create secret generic woocommerce-api-keys -n "${NAMESPACE}" \
    --from-literal=CONSUMER_KEY="ck_$(openssl rand -hex 20)" \
    --from-literal=CONSUMER_SECRET="cs_$(openssl rand -hex 20)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] woocommerce-api-keys created"
fi

if secret_exists woo-mcp-auth; then
  echo "  [✓] woo-mcp-auth exists"
else
  kubectl create secret generic woo-mcp-auth -n "${NAMESPACE}" \
    --from-literal=MCP_AUTH_TOKEN="$(rand_str)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] woo-mcp-auth created"
fi

if secret_exists gateway-encryption-key; then
  echo "  [✓] gateway-encryption-key exists"
else
  kubectl create secret generic gateway-encryption-key -n "${NAMESPACE}" \
    --from-literal=ENCRYPTION_KEY="$(openssl rand -base64 32)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] gateway-encryption-key created"
fi

if secret_exists gateway-admin-credentials; then
  echo "  [✓] gateway-admin-credentials exists"
else
  kubectl create secret generic gateway-admin-credentials -n "${NAMESPACE}" \
    --from-literal=ADMIN_EMAIL="admin@tollbooth.local" \
    --from-literal=ADMIN_PASSWORD="$(rand_str)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] gateway-admin-credentials created"
fi

# Temporary until Stage 4b replaces the bootstrap token with API-key authentication.
if secret_exists gateway-bootstrap-token; then
  echo "  [✓] gateway-bootstrap-token exists"
else
  kubectl create secret generic gateway-bootstrap-token -n "${NAMESPACE}" \
    --from-literal=BOOTSTRAP_TOKEN="tb_boot_$(openssl rand -hex 16)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] gateway-bootstrap-token created"
fi

echo "==> Required secrets are ready."