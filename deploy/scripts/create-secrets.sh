#!/usr/bin/env bash
# create-secrets.sh – Idempotently generate Kubernetes secrets for Tollbooth
#
# Generates random credentials for MariaDB, PostgreSQL, WordPress admin,
# and WooCommerce API keys if they do not already exist.
#
# SECURITY: Never prints passwords to stdout or stderr, and never writes
# secret values to repository files.

set -euo pipefail

NAMESPACE="${1:-${NAMESPACE:-tollbooth}}"

echo "==> Ensuring namespace '${NAMESPACE}' exists..."
kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1 || kubectl create namespace "${NAMESPACE}" >/dev/null

# Helper function to generate a secure random alphanumeric string
rand_str() {
  openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 24
}

# 1. MariaDB credentials
if kubectl get secret mariadb-credentials -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'mariadb-credentials' already exists in '${NAMESPACE}'"
else
  ROOT_PW=$(rand_str)
  USER_PW=$(rand_str)
  kubectl create secret generic mariadb-credentials \
    --namespace="${NAMESPACE}" \
    --from-literal=MYSQL_ROOT_PASSWORD="${ROOT_PW}" \
    --from-literal=MYSQL_USER="wordpress" \
    --from-literal=MYSQL_PASSWORD="${USER_PW}" \
    --from-literal=MYSQL_DATABASE="wordpress" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'mariadb-credentials' created in '${NAMESPACE}'"
fi

# 2. PostgreSQL credentials
if kubectl get secret postgres-credentials -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'postgres-credentials' already exists in '${NAMESPACE}'"
else
  PG_PW=$(rand_str)
  kubectl create secret generic postgres-credentials \
    --namespace="${NAMESPACE}" \
    --from-literal=POSTGRES_USER="tollbooth" \
    --from-literal=POSTGRES_PASSWORD="${PG_PW}" \
    --from-literal=POSTGRES_DB="tollbooth" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'postgres-credentials' created in '${NAMESPACE}'"
fi

# 3. WordPress Admin credentials
if kubectl get secret wordpress-admin-credentials -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'wordpress-admin-credentials' already exists in '${NAMESPACE}'"
else
  WP_ADMIN_PW=$(rand_str)
  kubectl create secret generic wordpress-admin-credentials \
    --namespace="${NAMESPACE}" \
    --from-literal=ADMIN_USER="admin" \
    --from-literal=ADMIN_PASSWORD="${WP_ADMIN_PW}" \
    --from-literal=ADMIN_EMAIL="admin@example.com" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'wordpress-admin-credentials' created in '${NAMESPACE}'"
fi

# 4. WooCommerce REST API keys
if kubectl get secret woocommerce-api-keys -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'woocommerce-api-keys' already exists in '${NAMESPACE}'"
else
  # WooCommerce consumer key starts with ck_, consumer secret starts with cs_
  CK="ck_$(openssl rand -hex 20)"
  CS="cs_$(openssl rand -hex 20)"
  kubectl create secret generic woocommerce-api-keys \
    --namespace="${NAMESPACE}" \
    --from-literal=CONSUMER_KEY="${CK}" \
    --from-literal=CONSUMER_SECRET="${CS}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'woocommerce-api-keys' created in '${NAMESPACE}'"
fi

# 5. woo-mcp static Bearer auth token
if kubectl get secret woo-mcp-auth -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'woo-mcp-auth' already exists in '${NAMESPACE}'"
else
  AUTH_TOKEN=$(rand_str)
  kubectl create secret generic woo-mcp-auth \
    --namespace="${NAMESPACE}" \
    --from-literal=MCP_AUTH_TOKEN="${AUTH_TOKEN}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'woo-mcp-auth' created in '${NAMESPACE}'"
fi

# 6. Gateway AES-256-GCM encryption key
if kubectl get secret gateway-encryption-key -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'gateway-encryption-key' already exists in '${NAMESPACE}'"
else
  # 32 random bytes (256-bit key) base64-encoded
  ENC_KEY=$(openssl rand -base64 32)
  kubectl create secret generic gateway-encryption-key \
    --namespace="${NAMESPACE}" \
    --from-literal=ENCRYPTION_KEY="${ENC_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'gateway-encryption-key' created in '${NAMESPACE}'"
fi

# 7. Gateway temporary bootstrap token
if kubectl get secret gateway-bootstrap-token -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "  [✓] Secret 'gateway-bootstrap-token' already exists in '${NAMESPACE}'"
else
  BOOTSTRAP_TOKEN="tb_boot_$(openssl rand -hex 16)"
  kubectl create secret generic gateway-bootstrap-token \
    --namespace="${NAMESPACE}" \
    --from-literal=BOOTSTRAP_TOKEN="${BOOTSTRAP_TOKEN}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "  [+] Secret 'gateway-bootstrap-token' created in '${NAMESPACE}'"
fi

echo "==> All required secrets are verified in '${NAMESPACE}'."
