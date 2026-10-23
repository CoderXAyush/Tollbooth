# Tollbooth

A WooCommerce hosting platform with a team MCP (Model Context Protocol) gateway on Kubernetes. Manage WooCommerce stores through AI-powered tools with role-based access, audit logging, and a unified chat interface.

## Status

| Chunk | Description | Status |
|-------|-------------|--------|
| 0 | Foundations, contracts, repo scaffold | ✅ Done |
| 1 | Cluster bootstrap, WordPress + WooCommerce deploy | 🔲 Not started |
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

## Local Setup

> Coming in Chunk 1 — cluster creation with kind, Helm install, WordPress + WooCommerce deployment.

## Production / VPS Setup

> Coming in Chunk 6.

## How to Place an Order

> Coming in Chunk 1 (WooCommerce store will be live).

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
