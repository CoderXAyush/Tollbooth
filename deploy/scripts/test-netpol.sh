#!/usr/bin/env bash
# test-netpol.sh – Prove Kubernetes NetworkPolicy enforcement in KinD
#
# Steps:
# 1. Creates a throwaway namespace (netpol-test)
# 2. Deploys a target HTTP server pod and Service
# 3. Applies a default-deny ingress NetworkPolicy
# 4. Proves an in-namespace client is blocked (connection timeout)
# 5. Applies an allow NetworkPolicy specifically for the client
# 6. Proves client can now reach the server (HTTP 200)
# 7. Cleans up the test namespace

set -euo pipefail

TEST_NS="netpol-test"

echo "==> [1/7] Setting up throwaway namespace '${TEST_NS}'..."
kubectl create namespace "${TEST_NS}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

cleanup() {
  echo "==> [7/7] Cleaning up namespace '${TEST_NS}'..."
  kubectl delete namespace "${TEST_NS}" --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> [2/7] Deploying target HTTP server and service..."
cat <<EOF | kubectl apply -n "${TEST_NS}" -f - >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: target-server
  labels:
    app: target-server
spec:
  containers:
    - name: server
      image: nginx:alpine
      ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: target-svc
spec:
  selector:
    app: target-server
  ports:
    - port: 80
      targetPort: 80
EOF

echo "    Waiting for target-server to be ready..."
kubectl wait --namespace "${TEST_NS}" --for=condition=ready pod/target-server --timeout=60s >/dev/null

echo "==> [3/7] Applying default-deny ingress NetworkPolicy..."
cat <<EOF | kubectl apply -n "${TEST_NS}" -f - >/dev/null
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-ingress
spec:
  podSelector: {}
  policyTypes:
    - Ingress
EOF

echo "==> [4/7] Testing traffic with default-deny (should be BLOCKED)..."
set +e
BLOCKED_OUTPUT=$(kubectl run curler -n "${TEST_NS}" --labels="app=curler" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s --max-time 3 http://target-svc 2>&1)
CURL_STATUS=$?
set -e

if [ ${CURL_STATUS} -ne 0 ]; then
  echo "    [PASS] Traffic successfully BLOCKED by NetworkPolicy (exit code: ${CURL_STATUS})."
else
  echo "    [FAIL] Traffic was NOT blocked! Output: ${BLOCKED_OUTPUT}"
  exit 1
fi

echo "==> [5/7] Applying allow NetworkPolicy for 'app=curler'..."
cat <<EOF | kubectl apply -n "${TEST_NS}" -f - >/dev/null
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-curler-to-target
spec:
  podSelector:
    matchLabels:
      app: target-server
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: curler
EOF

echo "==> [6/7] Testing traffic with allow policy (should SUCCEED)..."
ALLOWED_OUTPUT=$(kubectl run curler2 -n "${TEST_NS}" --labels="app=curler" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -o /dev/null -w "%{http_code}" --max-time 5 http://target-svc 2>&1)

if echo "${ALLOWED_OUTPUT}" | grep -q "200"; then
  echo "    [PASS] Traffic successfully ALLOWED by NetworkPolicy (HTTP status: 200)."
else
  echo "    [FAIL] Expected HTTP 200, got: '${ALLOWED_OUTPUT}'"
  exit 1
fi

echo "==> NetworkPolicy enforcement is verified successfully!"
