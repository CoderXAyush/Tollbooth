# Tollbooth

A WooCommerce hosting platform with a team MCP (Model Context Protocol) gateway on Kubernetes. Manage WooCommerce stores through AI-powered tools with role-based access, audit logging, and a unified chat interface.

## Status

| Chunk | Description | Status |
|-------|-------------|--------|
| 0 | Foundations, contracts, repo scaffold | ✅ Done |
| 1 | Cluster bootstrap, WordPress + WooCommerce deploy | ✅ Done |
| 2 | woo-mcp server implementation | 🔲 Not started |
| 3 | Gateway MCP aggregator + auth | 🔲 Not started |
| 4 | Dashboard UI | 🔲 Not started |
| 5 | Chat agent | 🔲 Not started |
| 6 | Production deployment (VPS/k3s) | 🔲 Not started |

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

---

## How to Connect an External MCP Client

> Coming in Chunk 3.

## How to Use the Chat Agent

> Coming in Chunk 5.

## Design Docs

- **[DESIGN.md](docs/DESIGN.md)** — Architecture, tool routing, failure handling, auth model, Kubernetes layout
- **[API.md](docs/API.md)** — REST API reference and MCP endpoint spec
- **[SCHEMA.sql](docs/SCHEMA.sql)** — PostgreSQL schema for gateway state
- **[THREAT_MODEL.md](docs/THREAT_MODEL.md)** — Security considerations

## License

MIT — see [LICENSE](LICENSE).
