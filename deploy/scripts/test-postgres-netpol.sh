#!/usr/bin/env bash
# test-postgres-netpol.sh – Prove PostgreSQL NetworkPolicy isolation in Tollbooth
#
# Steps:
# 1. Proves gateway can reach postgres (readiness probe / connection test succeeds)
# 2. Proves non-gateway pod (woo-mcp) cannot reach postgres (times out with exit code 28)

set -euo pipefail

NAMESPACE="${1:-tollbooth}"

echo "==> [1/2] Testing PostgreSQL access from gateway (should SUCCEED)..."
GATEWAY_POD=$(kubectl get pod -n "${NAMESPACE}" -l app.kubernetes.io/name=gateway -o jsonpath='{.items[0].metadata.name}')
READYZ_OUT=$(kubectl exec -n "${NAMESPACE}" "${GATEWAY_POD}" -- wget -qO- http://127.0.0.1:8080/readyz 2>&1)

if echo "${READYZ_OUT}" | grep -q "ready"; then
  echo "    [PASS] Gateway successfully connected to PostgreSQL (${READYZ_OUT})."
else
  echo "    [FAIL] Gateway could not connect to PostgreSQL: ${READYZ_OUT}"
  exit 1
fi

echo "==> [2/2] Testing PostgreSQL access from non-gateway pod (woo-mcp) (should TIMEOUT with exit 28)..."
WOO_POD=$(kubectl get pod -n "${NAMESPACE}" -l app.kubernetes.io/name=woo-mcp -o jsonpath='{.items[0].metadata.name}')

set +e
DEBUG_OUT=$(kubectl debug -q -i --image=curlimages/curl:8.11.1 "pod/${WOO_POD}" -n "${NAMESPACE}" -- sh -c 'curl -s --connect-timeout 2 http://postgres:5432 || echo "CURL_EXIT=$?"' 2>&1)
set -e

if echo "${DEBUG_OUT}" | grep -q "CURL_EXIT=28"; then
  echo "    [PASS] Non-gateway pod was BLOCKED from reaching PostgreSQL with timeout exit code 28."
else
  echo "    [FAIL] Expected timeout exit code 28, but got output: ${DEBUG_OUT}"
  exit 1
fi

echo "==> PostgreSQL NetworkPolicy isolation successfully verified!"
