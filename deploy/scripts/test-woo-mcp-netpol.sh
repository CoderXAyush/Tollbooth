#!/usr/bin/env bash
# Proves the deployed woo-mcp NetworkPolicy permits only required traffic.
# No secret is decoded or placed in a local command line.

set -euo pipefail

NAMESPACE="${1:-tollbooth}"
WOO_SERVICE="woo-mcp-woo-mcp"
WORDPRESS_SERVICE="store-wordpress"
MARIADB_SERVICE="store-mariadb"

delete_pod() {
  kubectl delete pod "$1" -n "${NAMESPACE}" --ignore-not-found --wait=false >/dev/null
}

run_timeout_test() {
  local name="$1"
  local labels="$2"
  local url="$3"

  set +e
  local output
  output="$(kubectl run "${name}" -n "${NAMESPACE}" \
    --labels="${labels}" \
    --image=curlimages/curl:8.11.1 \
    --restart=Never --rm -i \
    --command -- curl -sS --connect-timeout 3 --max-time 3 "${url}" 2>&1)"
  local status=$?
  set -e

  if [[ ${status} -ne 0 ]] && grep -qE 'timed out|Timeout|28' <<<"${output}"; then
    echo "    [PASS] ${name} was blocked by timeout."
    return 0
  fi

  echo "    [FAIL] ${name} was expected to time out; exit=${status}; output=${output}" >&2
  return 1
}

echo "==> [1/5] Gateway-labelled pod can reach woo-mcp health endpoint..."
allowed="$(kubectl run netpol-gateway-client -n "${NAMESPACE}" \
  --labels="app.kubernetes.io/name=gateway" \
  --image=curlimages/curl:8.11.1 \
  --restart=Never --rm -i \
  --command -- curl -sS -o /dev/null -w '%{http_code}' --max-time 5 \
  "http://${WOO_SERVICE}:8081/healthz" 2>&1)"

if grep -q '200' <<<"${allowed}"; then
  echo "    [PASS] Gateway-labelled pod reached woo-mcp."
else
  echo "    [FAIL] Expected HTTP 200; got: ${allowed}" >&2
  exit 1
fi

echo "==> [2/5] Non-gateway pod cannot reach woo-mcp..."
run_timeout_test \
  "netpol-unauth-client" \
  "app.kubernetes.io/name=unauthorized" \
  "http://${WOO_SERVICE}:8081/healthz"

echo "==> [3/5] woo-mcp identity can call the store without exposing its bearer token..."
cat <<'EOF' | kubectl apply -n "${NAMESPACE}" -f - >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: netpol-woo-store-client
  labels:
    app.kubernetes.io/name: gateway
spec:
  restartPolicy: Never
  containers:
    - name: client
      image: curlimages/curl:8.11.1
      env:
        - name: MCP_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: woo-mcp-auth
              key: MCP_AUTH_TOKEN
      command:
        - /bin/sh
        - -ec
        - |
          curl -sS --max-time 10 \
            -X POST \
            -H "Authorization: Bearer ${MCP_AUTH_TOKEN}" \
            -H "Content-Type: application/json" \
            -H "Accept: application/json, text/event-stream" \
            -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_products","arguments":{"per_page":1}}}' \
            "http://woo-mcp-woo-mcp:8081/mcp"
EOF

if ! kubectl wait -n "${NAMESPACE}" --for=jsonpath='{.status.phase}'=Succeeded \
  pod/netpol-woo-store-client --timeout=30s >/dev/null; then
  kubectl logs -n "${NAMESPACE}" netpol-woo-store-client >&2 || true
  delete_pod netpol-woo-store-client
  echo "    [FAIL] Store-call pod did not complete." >&2
  exit 1
fi

store_call="$(kubectl logs -n "${NAMESPACE}" netpol-woo-store-client)"
delete_pod netpol-woo-store-client

if grep -q '"isError":true' <<<"${store_call}" || ! grep -q 'Tollbooth Hoodie' <<<"${store_call}"; then
  echo "    [FAIL] woo-mcp could not retrieve the product catalog." >&2
  exit 1
fi
echo "    [PASS] woo-mcp reached WordPress through its allowed egress rule."

echo "==> [4/5] MariaDB rejects a non-WordPress/non-bootstrap pod by timeout..."
run_timeout_test \
  "netpol-mariadb-client" \
  "app.kubernetes.io/name=woo-mcp,app.kubernetes.io/instance=woo-mcp" \
  "http://${MARIADB_SERVICE}:3306"

echo "==> [5/5] woo-mcp identity cannot reach the public internet..."
run_timeout_test \
  "netpol-internet-client" \
  "app.kubernetes.io/name=woo-mcp,app.kubernetes.io/instance=woo-mcp" \
  "http://1.1.1.1:80"

echo "==> All woo-mcp NetworkPolicy checks passed."