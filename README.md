# Tollbooth

A WooCommerce hosting platform with a team MCP (Model Context Protocol) gateway on Kubernetes. Manage WooCommerce stores through AI-powered tools with role-based access, audit logging, and a unified chat interface.

## Status

| Chunk | Description | Status |
|---|---|---|
| 0 | Foundations, contracts, repo scaffold | ✅ Done |
| 1 | Cluster bootstrap, WordPress + WooCommerce deploy | ✅ Done |
| 2 | woo-mcp server implementation | ✅ Done |
| 3 | Gateway core (aggregation, routing, failure isolation) | ✅ Done (`chunk-3`) |
| 4 | Users, roles, sessions, API keys, audit log | 🟡 In progress (Stage 4a: session auth and admin access) |
| 5 | Dashboard UI | 🔲 Not started |
| 6 | Chat agent | 🔲 Not started |
| 7 | Production deployment (VPS/k3s) | 🔲 Not started |

## Architecture Overview

```
┌─────────────┐   ┌─────────────┐
│   Browser    │   │ External    │
│  (Dashboard) │   │ MCP Client  │
└──────┬───────┘   └──────┬──────┘
       │ HTTPS            │ MCP Streamable HTTP
       ▼                  ▼
┌─────────────────────────────────────┐
│          Kubernetes Cluster         │
│  ┌───────────┐  ┌────────────────┐  │
│  │ Dashboard  │  │    Gateway     │  │
│  │ (React+   │  │ (Go, REST +   │  │
│  │  Nginx)   │  │  MCP server)  │  │
│  └───────────┘  └───────┬────────┘  │
│                         │ MCP client│
│                  ┌──────▼────────┐  │
│                  │   woo-mcp     │  │
│                  │ (Go, internal)│  │
│                  └──────┬────────┘  │
│                         │ REST API  │
│  ┌───────────┐  ┌──────▼────────┐  │
│  │ PostgreSQL │  │  WordPress + │  │
│  │ (gateway   │  │ WooCommerce  │  │
│  │  state)    │  │  + MariaDB   │  │
│  └───────────┘  └───────────────┘  │
└─────────────────────────────────────┘
```

See [docs/DESIGN.md](docs/DESIGN.md) for detailed architecture, routing, failure handling, and security design.

## Repository Layout

```
.
├── services/
│   ├── gateway/          # Go – MCP aggregator, REST API, auth
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   └── Dockerfile
│   └── woo-mcp/          # Go – WooCommerce MCP server
│       ├── cmd/main.go
│       ├── internal/
│       └── Dockerfile
├── dashboard/             # React + Vite + TypeScript
│   ├── src/
│   ├── nginx.conf
│   └── Dockerfile
├── deploy/
│   ├── helm/tollbooth/   # Helm umbrella chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   ├── values-local.yaml
│   │   ├── values-prod.yaml
│   │   ├── templates/    # Ingress
│   │   └── charts/       # Subcharts: gateway, woo-mcp, dashboard, postgres, store
│   └── kind/             # kind cluster config + creation script
├── docs/
│   ├── DESIGN.md         # Architecture and design decisions
│   ├── API.md            # REST API + MCP endpoint reference
│   ├── SCHEMA.sql        # PostgreSQL schema
│   └── THREAT_MODEL.md   # Security threat model
├── Makefile
├── LICENSE               # MIT
└── README.md
```

## Prerequisites

| Tool | Version tested | Install |
|------|---------------|---------|
| Go | 1.25+ | [go.dev/dl](https://go.dev/dl/) |
| Node.js | 22.x LTS | [nodejs.org](https://nodejs.org/) |
| Docker | 29.x+ | [docs.docker.com](https://docs.docker.com/get-docker/) |
| Helm | 3.16+ | [helm.sh/docs/intro/install](https://helm.sh/docs/intro/install/) |
| kind | 0.25+ | [kind.sigs.k8s.io](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) |
| kubectl | 1.31+ | [kubernetes.io/docs/tasks/tools](https://kubernetes.io/docs/tasks/tools/) |

## Quick Start (What Works Today — Chunk 0)

### Build everything

```bash
# Build Go services
make build-gateway
make build-woo-mcp

# Build dashboard
make build-dashboard

# Lint Go code
make lint-go

# Lint Helm charts
make helm-lint
```

### Docker images

```bash
make docker-build
```

### Helm template (dry-run)

```bash
# Preview local manifests
make helm-template-local

# Preview prod manifests
make helm-template-prod
```

### Run a Go service locally

```bash
cd services/gateway && go run ./cmd/main.go
# → http://localhost:8080/healthz
# → http://localhost:8080/readyz
```

## Local Setup (Kind + Ingress + Store)

### 1. Bootstrap KinD Cluster
```bash
# Creates kind cluster with ingress-ready node labels & host port 80/443 mappings,
# and installs pinned NGINX Ingress controller (v1.11.3)
make cluster-up
```

### 2. Verify NetworkPolicy Enforcement
```bash
# Proves kindnet actively blocks unauthorized ingress traffic and allows authorized pods
make test-netpol
```

### 3. Generate Kubernetes Secrets
```bash
# Idempotently generates MariaDB, Postgres, WP admin, and WooCommerce REST credentials
make secrets
```

### 4. Deploy Store Chart Standalone (or via Umbrella)
```bash
# Deploy the standalone store chart into the tollbooth namespace
helm install store deploy/helm/tollbooth/charts/store -n tollbooth

# Verify pods, services, PVCs, and Ingress:
kubectl get pods,pvc,ingress,svc -n tollbooth
```

---

## Local Domain & Offline Access

Tollbooth uses `*.127.0.0.1.nip.io` wildcard DNS, which resolves to `127.0.0.1` automatically:
- **Storefront & Admin:** [http://store.127.0.0.1.nip.io](http://store.127.0.0.1.nip.io)
- **API & MCP Gateway:** [http://api.127.0.0.1.nip.io](http://api.127.0.0.1.nip.io)
- **Dashboard:** [http://dashboard.127.0.0.1.nip.io](http://dashboard.127.0.0.1.nip.io)

### Offline Demo Fallback (`/etc/hosts`)
If running without internet or DNS access, map the domains locally:
```bash
echo "127.0.0.1 store.127.0.0.1.nip.io dashboard.127.0.0.1.nip.io api.127.0.0.1.nip.io" | sudo tee -a /etc/hosts
```

---

## How to Place an Order

1. Open the storefront catalog: [http://store.127.0.0.1.nip.io/shop/](http://store.127.0.0.1.nip.io/shop/)
2. Locate the seeded product **"Tollbooth Hoodie"** ($49.99) and click **"Add to cart"**.
3. Navigate to the Cart: [http://store.127.0.0.1.nip.io/cart/](http://store.127.0.0.1.nip.io/cart/) and click **"Proceed to checkout"**.
4. On the Checkout page ([http://store.127.0.0.1.nip.io/checkout/](http://store.127.0.0.1.nip.io/checkout/)):
   - Enter standard billing details (Name, Address, Email).
   - Under Payment Methods, verify **"Cash on delivery"** is selected.
   - Click **"Place order"**.
5. The confirmation screen will display: *"Thank you. Your order has been received."* with a unique Order Number.

---

## How to Log into wp-admin

1. Visit the WordPress login page: [http://store.127.0.0.1.nip.io/wp-login.php](http://store.127.0.0.1.nip.io/wp-login.php)
2. Retrieve the admin username:
   ```bash
   kubectl get secret wordpress-admin-credentials -n tollbooth -o jsonpath='{.data.ADMIN_USER}' | base64 -d && echo ""
   # Default: admin
   ```
3. Retrieve the generated admin password:
   ```bash
   kubectl get secret wordpress-admin-credentials -n tollbooth -o jsonpath='{.data.ADMIN_PASSWORD}' | base64 -d && echo ""
   ```
4. In the WordPress Admin Dashboard, navigate to **WooCommerce → Orders** to review placed orders.

---

## In-Cluster REST API Verification

Test the pre-seeded WooCommerce REST API credentials from a temporary pod inside the cluster:

```bash
# Extract generated consumer key and secret
CK=$(kubectl get secret woocommerce-api-keys -n tollbooth -o jsonpath='{.data.CONSUMER_KEY}' | base64 -d)
CS=$(kubectl get secret woocommerce-api-keys -n tollbooth -o jsonpath='{.data.CONSUMER_SECRET}' | base64 -d)

# Query products with valid credentials (returns HTTP 200)
kubectl run curl-test -n tollbooth --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -u "${CK}:${CS}" -H "X-Forwarded-Proto: https" "http://store-wordpress/wp-json/wc/v3/products"

# Query with invalid secret (returns HTTP 401 Unauthorized)
kubectl run curl-test -n tollbooth --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -u "${CK}:bad_secret" -H "X-Forwarded-Proto: https" "http://store-wordpress/wp-json/wc/v3/products"
```

---

## woo-mcp Server & Tools (Chunk 2)

The `woo-mcp` service exposes WooCommerce store operations as Model Context Protocol (MCP) tools over Streamable HTTP (`/mcp`) with Bearer token authentication.

### Deploy woo-mcp

```bash
# Build Docker image and load into kind
docker build -t tollbooth/woo-mcp:dev services/woo-mcp
kind load docker-image tollbooth/woo-mcp:dev --name tollbooth

# Deploy or upgrade woo-mcp subchart
helm upgrade --install woo-mcp deploy/helm/tollbooth/charts/woo-mcp -n tollbooth
```

### Run Unit Tests with Race Detection

```bash
cd services/woo-mcp && go test -race ./...
```

### Validate NetworkPolicy Isolation

```bash
# Proves:
# 1. Gateway-labeled pods CAN reach woo-mcp:8081
# 2. Unauthorized pods are BLOCKED by NetworkPolicy
# 3. woo-mcp egress to store-wordpress:80 succeeds
# 4. Direct MariaDB:3306 access is BLOCKED
make test-woo-mcp-netpol
```

### Test MCP Protocol from Gateway Pod

```bash
# Extract static Bearer auth token
TOKEN=$(kubectl get secret woo-mcp-auth -n tollbooth -o jsonpath='{.data.MCP_AUTH_TOKEN}' | base64 -d)

# 1. Initialize MCP Session
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"gateway-curl","version":"1.0.0"}}}' \
  http://woo-mcp-woo-mcp:8081/mcp

# 2. List Available Tools
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  http://woo-mcp-woo-mcp:8081/mcp

# 3. Create a Product ("Test Mug", $12.50)
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_product","arguments":{"name":"Test Mug","regular_price":"12.50","description":"Ceramic developer mug"}}}' \
  http://woo-mcp-woo-mcp:8081/mcp

# 4. List Orders Created Today
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_orders","arguments":{"after":"2026-10-25T00:00:00Z"}}}' \
  http://woo-mcp-woo-mcp:8081/mcp

# 5. Get Order Details (ID 14)
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_order","arguments":{"id":14}}}' \
  http://woo-mcp-woo-mcp:8081/mcp

# 6. Update Order Status
kubectl run gateway-curl -n tollbooth --labels="app.kubernetes.io/name=gateway" --image=curlimages/curl --restart=Never --rm -i \
  --command -- curl -s -X POST \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update_order_status","arguments":{"id":14,"status":"processing"}}}' \
  http://woo-mcp-woo-mcp:8081/mcp
```

### Test via MCP Inspector

```bash
# Forward woo-mcp service to localhost:
kubectl port-forward svc/woo-mcp-woo-mcp -n tollbooth 8081:8081

# In another terminal, connect with the MCP Inspector:
npx @modelcontextprotocol/inspector
# Set transport to Streamable HTTP:
# URL: http://localhost:8081/mcp
# Custom Headers: Authorization: Bearer <TOKEN>
```

---

## Gateway Core: MCP Aggregator & Registry (Chunk 3)

The Gateway exposes an aggregated Model Context Protocol (MCP) endpoint over Streamable HTTP at `/mcp`, and a REST API for server management at `/api/servers`.

### 1. Retrieve the Temporary Bootstrap Token
```bash
BOOTSTRAP_TOKEN=$(kubectl get secret gateway-bootstrap-token -n tollbooth -o jsonpath='{.data.BOOTSTRAP_TOKEN}' | base64 -d)
echo "Bootstrap Token: ${BOOTSTRAP_TOKEN}"
```

### 2. Query Aggregated Tools (`tools/list`)
All tools are namespaced by server slug (`<slug>__<tool>`). Unreachable or disabled servers' tools are automatically omitted:
```bash
curl -s -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://gateway.127.0.0.1.nip.io/mcp
```

### 3. Call an Aggregated Tool (`tools/call`)
Calls are routed by prefix to the target upstream with context timeouts, failure isolation, and sanitised error messages:
```bash
# List products through the primary WooCommerce upstream (slug: woo)
curl -s -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"woo__list_products","arguments":{"per_page":2}}}' \
  http://gateway.127.0.0.1.nip.io/mcp

# Create a new product through the gateway
curl -s -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"woo__create_product","arguments":{"name":"Gateway Mug","regular_price":"14.99"}}}' \
  http://gateway.127.0.0.1.nip.io/mcp
```

### 4. Register and Manage Upstream Servers via REST API
```bash
# Register a new upstream server
curl -s -X POST -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"slug":"woo-b","name":"Secondary Store","url":"http://woo-b-woo-mcp:8081/mcp","credential":"<UPSTREAM_SECRET>","enabled":true}' \
  http://gateway.127.0.0.1.nip.io/api/servers

# List registered servers and their health status
curl -s -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  http://gateway.127.0.0.1.nip.io/api/servers

# Test connection to an upstream server
curl -s -X POST -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  http://gateway.127.0.0.1.nip.io/api/servers/<SERVER_ID>/test

# Disable a server (omits its tools from /mcp tools/list)
curl -s -X PUT -H "Authorization: Bearer ${BOOTSTRAP_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"enabled":false}' \
  http://gateway.127.0.0.1.nip.io/api/servers/<SERVER_ID>
```

---

## Troubleshooting

1. **MariaDB TLS Negotiation (`TLS/SSL error: self-signed certificate`):**
   - *Symptom:* MariaDB client hangs or fails during automated startup checks.
   - *Cause:* MariaDB 11.4 generates a self-signed TLS certificate by default. Over internal cluster networks (`store-mariadb:3306`), alpine-based clients fail cert verification.
   - *Fix:* Automated probes and startup scripts explicitly pass `--ssl=0` for in-cluster health pings.

2. **WordPress Container vs. CLI UID Mismatch:**
   - *Symptom:* `wp core download` fails with `Permission denied` on `wp-content/index.php`.
   - *Cause:* Debian-based Apache (`wordpress`) uses UID 33 (`www-data`), while Alpine-based WP-CLI (`wordpress:cli`) uses UID 82.
   - *Fix:* The bootstrap Job runs with `securityContext: { runAsUser: 0 }`, `HOME: /tmp`, passes `--allow-root`, and finishes with `chown -R 33:33 /var/www/html/wp-content`.

3. **WooCommerce REST 401 over Internal Plain HTTP:**
   - *Symptom:* REST API calls over `http://store-wordpress/...` fail with 401 `woocommerce_rest_cannot_view`.
   - *Cause:* WooCommerce disables Basic Authentication unless `is_ssl()` is true.
   - *Fix:* WordPress handles `HTTP_X_FORWARDED_PROTO: https` in `wp-config.php`. Clients pass `-H "X-Forwarded-Proto: https"` to activate Basic Auth over in-cluster HTTP.

4. **woo-mcp 401 Unauthorized:**
   - *Symptom:* `POST /mcp` returns HTTP 401 with `{"error":"unauthorized: invalid bearer token"}`.
   - *Cause:* Missing or mismatched Bearer token in the request header.
   - *Fix:* Verify token from `kubectl get secret woo-mcp-auth -n tollbooth -o jsonpath='{.data.MCP_AUTH_TOKEN}' | base64 -d`.

5. **NetworkPolicy Connection Timeout (exit code 28):**
   - *Symptom:* Calls to `http://woo-mcp-woo-mcp:8081` hang and time out.
   - *Cause:* The calling pod does not possess the required label `app.kubernetes.io/name: gateway`.
   - *Fix:* Apply label `app.kubernetes.io/name: gateway` to client pods communicating with `woo-mcp`.

6. **Store Offline / WooCommerce Unreachable:**
   - *Symptom:* `woo-mcp` returns `isError: true` with `WooCommerce request failed: ... connection refused`.
   - *Behavior:* `woo-mcp` remains healthy and responsive (`/healthz` and `/readyz` return 200). Once WordPress restarts and becomes ready, tool calls immediately succeed without restarting `woo-mcp`.

7. **Multi-Release Helm Chart Service Conflicts:**
   - *Symptom:* Installing a second instance of `woo-mcp` chart (e.g. `woo-b`) failed with `Service "woo-mcp" already exists`.
   - *Cause:* Subchart defined a static un-templated Service name `woo-mcp` in addition to `{{ .Release.Name }}-woo-mcp`.
   - *Fix:* Scoped the un-prefixed alias service conditionally to `.Release.Name == "woo-mcp"` and added `app.kubernetes.io/instance: {{ .Release.Name }}` pod selectors.

8. **NGINX Ingress SSE / Chunk Buffering:**
   - *Symptom:* Streamable HTTP responses to MCP clients block until the full response finishes instead of streaming events in real time.
   - *Cause:* Ingress NGINX buffers proxy responses by default.
   - *Fix:* Added `nginx.ingress.kubernetes.io/proxy-buffering: "off"` and extended read timeouts to 3600 seconds.

## Gateway Authentication (Chunk 4a)

The gateway supports session-based authentication and administrator authorization.

### Authentication endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/api/auth/login` | Authenticate with email and password |
| `POST` | `/api/auth/logout` | Revoke the current session |
| `GET` | `/api/me` | Return the current authenticated identity |

### Authorization

The `/api/servers` endpoints require an authenticated administrator.

- `401 Unauthorized`: missing or invalid authentication.
- `403 Forbidden`: authenticated user lacks administrator privileges.

### Security controls

- Password verification uses bcrypt with cost 12.
- Login attempts are limited to 10 per IP address per minute.
- Invalid credentials receive a generic error response.
- Unknown-user attempts perform a dummy bcrypt comparison.
- Database lookup failures return an internal error.
- User identities and sessions are stored in PostgreSQL.

### Database migrations and tests

Identity and session tables are managed by migration
`000002_create_identity_tables.up.sql`. Gateway tables for Chunk 3 are managed
by `000001_create_chunk3_tables.up.sql`.

Run validation from `services/gateway`:

```bash
go test ./...
go test -race ./...
go vet ./...
---

## How to Connect an External MCP Client
Connect any MCP-compliant client (e.g. Claude Desktop, Cursor, MCP Inspector) to `http://gateway.127.0.0.1.nip.io/mcp` using Streamable HTTP transport with header `Authorization: Bearer <BOOTSTRAP_TOKEN>`.

## How to Use the Chat Agent

> Coming in Chunk 5.

## Design Docs

- **[DESIGN.md](docs/DESIGN.md)** — Architecture, tool routing, failure handling, auth model, Kubernetes layout
- **[API.md](docs/API.md)** — REST API reference and MCP endpoint spec
- **[SCHEMA.sql](docs/SCHEMA.sql)** — PostgreSQL schema for gateway state
- **[THREAT_MODEL.md](docs/THREAT_MODEL.md)** — Security considerations

## License

MIT — see [LICENSE](LICENSE).
