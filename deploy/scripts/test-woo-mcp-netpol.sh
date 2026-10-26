#!/usr/bin/env bash
# test-woo-mcp-netpol.sh – Prove NetworkPolicy enforcement for woo-mcp in tollbooth namespace
#
# Verifies:
# 1. Pods with label app.kubernetes.io/name=gateway CAN reach woo-mcp port 8081.
# 2. Pods without gateway label CANNOT reach woo-mcp port 8081 (connection timeout).
# 3. woo-mcp CAN reach store-wordpress port 80.
# 4. woo-mcp CANNOT reach store-mariadb port 3306 (blocked by egress/mariadb ingress netpol).

set -euo pipefail

NAMESPACE="tollbooth"

echo "==> [1/4] Testing allowed ingress from gateway-labeled pod to woo-mcp:8081..."
ALLOWED_OUTPUT=$(kubectl run netpol-gateway-client -n "${NAMESPACE}" \
  --labels="app.kubernetes.io/name=gateway" \
  --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -o /dev/null -w "%{http_code}" --max-time 5 http://woo-mcp-woo-mcp:8081/healthz 2>&1)

if echo "${ALLOWED_OUTPUT}" | grep -q "200"; then
  echo "    [PASS] Gateway-labeled pod reached woo-mcp successfully (HTTP 200)."
else
  echo "    [FAIL] Expected HTTP 200, got: '${ALLOWED_OUTPUT}'"
  exit 1
fi

echo "==> [2/4] Testing blocked ingress from non-gateway pod to woo-mcp:8081..."
set +e
BLOCKED_OUTPUT=$(kubectl run netpol-unauth-client -n "${NAMESPACE}" \
  --labels="app.kubernetes.io/name=unauthorized" \
  --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s --max-time 3 http://woo-mcp-woo-mcp:8081/healthz 2>&1)
EXIT_CODE=$?
set -e

if [ ${EXIT_CODE} -ne 0 ]; then
  echo "    [PASS] Unauthorized pod was BLOCKED as expected (exit code: ${EXIT_CODE})."
else
  echo "    [FAIL] Unauthorized pod was NOT blocked! Output: ${BLOCKED_OUTPUT}"
  exit 1
fi

echo "==> [3/4] Testing woo-mcp pod egress to store-wordpress:80 (via /mcp tool call)..."
# We verify egress by calling list_products via the gateway client. If egress were blocked, it would fail.
EGRESS_OUTPUT=$(kubectl run netpol-gateway-client -n "${NAMESPACE}" \
  --labels="app.kubernetes.io/name=gateway" \
  --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer $(kubectl get secret woo-mcp-auth -n ${NAMESPACE} -o jsonpath='{.data.MCP_AUTH_TOKEN}' | base64 -d)" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_products","arguments":{}}}' \
  http://woo-mcp-woo-mcp:8081/mcp 2>&1)

if echo "${EGRESS_OUTPUT}" | grep -q "Tollbooth Hoodie"; then
  echo "    [PASS] woo-mcp successfully reached WordPress store (received product catalog)."
else
  echo "    [FAIL] Store egress failed! Output: ${EGRESS_OUTPUT}"
  exit 1
fi

echo "==> [4/5] Testing blocked access to MariaDB:3306 from woo-mcp pod itself (asserting timeout)..."
WOO_POD=$(kubectl get pod -n "${NAMESPACE}" -l app.kubernetes.io/name=woo-mcp,app.kubernetes.io/instance=woo-mcp -o jsonpath='{.items[0].metadata.name}')

START_TS=$(date +%s)
MARIADB_OUTPUT=$(kubectl debug -n "${NAMESPACE}" "${WOO_POD}" -i --image=curlimages/curl -- sh -c "curl -s --connect-timeout 3 http://store-mariadb:3306; echo 'EXIT:'\$?" 2>&1)
END_TS=$(date +%s)
DURATION=$((END_TS - START_TS))

if echo "${MARIADB_OUTPUT}" | grep -q "EXIT:28"; then
  echo "    [PASS] Direct MariaDB access from woo-mcp was BLOCKED with TIMEOUT (exit code: 28, duration: ${DURATION}s)."
else
  echo "    [FAIL] Expected timeout exit code 28, got output: ${MARIADB_OUTPUT}"
  exit 1
fi

echo "==> [5/5] Testing blocked access to internet from woo-mcp pod itself (asserting timeout)..."
START_TS=$(date +%s)
INTERNET_OUTPUT=$(kubectl debug -n "${NAMESPACE}" "${WOO_POD}" -i --image=curlimages/curl -- sh -c "curl -s --connect-timeout 3 http://1.1.1.1:80; echo 'EXIT:'\$?" 2>&1)
END_TS=$(date +%s)
DURATION=$((END_TS - START_TS))

if echo "${INTERNET_OUTPUT}" | grep -q "EXIT:28"; then
  echo "    [PASS] Internet access from woo-mcp was BLOCKED with TIMEOUT (exit code: 28, duration: ${DURATION}s)."
else
  echo "    [FAIL] Expected timeout exit code 28, got output: ${INTERNET_OUTPUT}"
  exit 1
fi

echo "==> All woo-mcp NetworkPolicy tests PASSED!"
