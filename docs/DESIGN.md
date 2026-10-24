# Tollbooth – Design Document

## 1. Architecture Overview

```mermaid
graph TB
    subgraph "External"
        Browser["Browser / Dashboard"]
        ExtClient["External MCP Client"]
    end

    subgraph "Kubernetes Cluster"
        subgraph "Public (Ingress)"
            Dashboard["Dashboard<br/>React + Nginx"]
            Gateway["Gateway<br/>Go HTTP Server"]
        end

        subgraph "Internal Only (ClusterIP)"
            WooMCP["woo-mcp<br/>Go MCP Server"]
            WP["WordPress +<br/>WooCommerce"]
            MariaDB["MariaDB"]
            Postgres["PostgreSQL"]
        end
    end

    Browser -->|HTTPS| Dashboard
    Browser -->|REST API| Gateway
    ExtClient -->|MCP Streamable HTTP| Gateway

    Dashboard -->|/api/ proxy| Gateway
    Gateway -->|MCP Streamable HTTP| WooMCP
    Gateway -->|SQL| Postgres

    WooMCP -->|REST API<br/>consumer key/secret| WP
    WP -->|SQL| MariaDB

    style WooMCP fill:#f9a825,stroke:#f57f17
    style Gateway fill:#1e88e5,stroke:#1565c0,color:#fff
    style Dashboard fill:#43a047,stroke:#2e7d32,color:#fff
```

### Component Responsibilities

| Component | Role | Exposure |
|-----------|------|----------|
| **Gateway** | MCP client to upstreams, MCP server to consumers, REST API for dashboard, auth, audit logging | Ingress (public) |
| **woo-mcp** | MCP server exposing WooCommerce data as tools via Streamable HTTP | ClusterIP (internal only) |
| **Dashboard** | React SPA for admins/members: server management, tool browsing, chat agent, audit logs | Ingress (public) |
| **WordPress + WooCommerce** | The e-commerce store | ClusterIP (internal only) |
| **MariaDB** | WordPress/WooCommerce database | ClusterIP (internal only) |
| **PostgreSQL** | Gateway state: users, API keys, upstream configs, audit logs | ClusterIP (internal only) |

---

## 2. Tool Aggregation and Routing

### Naming Scheme: `<server_slug>__<tool_name>`

When the gateway lists tools from multiple upstream MCP servers, tool names can collide (e.g., two servers both expose `list_products`). We resolve this with a **mandatory prefix scheme**:

```
woo_store1__list_products
woo_store1__get_order
inventory__list_products
```

**Rules:**
1. The double-underscore `__` is the separator. It is forbidden in both slugs and tool names.
2. Server slugs are user-defined when registering an upstream, validated against: `^[a-z][a-z0-9_]{0,62}$` (lowercase, starts with letter, max 63 chars, single underscores only).
3. Tool names from upstreams are passed through as-is after validation: `^[a-zA-Z][a-zA-Z0-9_]{0,126}$`.
4. If a slug collides with an existing registered server, registration is rejected with HTTP 409.
5. Tool descriptions from upstreams are prefixed with `[server_slug] ` so the LLM can distinguish context.

### Slug collision handling

- Slugs are unique per gateway instance (enforced by a UNIQUE constraint in PostgreSQL).
- Attempting to register a slug that already exists returns `409 Conflict`.
- Renaming a slug is an atomic operation (UPDATE + tool cache invalidation).

### Illegal characters

- If a tool name from an upstream contains `__`, the gateway rejects the tool during discovery and logs a warning. The remaining tools from that upstream are still registered.
- If a tool name contains characters outside `[a-zA-Z0-9_]`, those characters are stripped and a warning is logged. If the resulting name is empty or collides, the tool is skipped.

---

## 3. Failure Handling

### Per-upstream timeouts

Every MCP client call to an upstream has a **configurable per-server timeout** (default: 30s). This is enforced with `context.WithTimeout` so a slow upstream cannot block the gateway's HTTP handler goroutine indefinitely.

### Server health states

Each upstream is tracked with one of three states:

| State | Meaning | Behaviour |
|-------|---------|-----------|
| **Connected** | Last health check / tool call succeeded | Tools listed normally |
| **Unreachable** | Last N consecutive calls failed (default N=3) | Tools still listed but marked with `status: unreachable`; calls attempted but with shorter timeout |
| **Disabled** | Admin manually disabled the server | Tools hidden from listings; calls rejected immediately |

State transitions are recorded in the `mcp_servers` table (`status`, `last_error`, `last_checked_at`).

### Partial listing behaviour

- `GET /api/tools` always returns **all tools from all Connected and Unreachable servers**. Each tool includes the server's current status.
- If one upstream times out during a `tools/list` refresh, the gateway returns **stale cached tools** for that upstream and includes a `"stale": true` flag.
- If an upstream has never been reachable, it returns zero tools for that server (not an error at the API level).
- Tool calls to an Unreachable server are attempted (the state might be transient) but with a reduced timeout (default: 10s vs 30s).

### Circuit breaker (future)

We may add a circuit breaker pattern in a later chunk. For now, the N-consecutive-failures model is sufficient for the project scope.

---

## 4. WooCommerce Integration Approach

### Decision: WooCommerce REST API with consumer key/secret

**Alternatives considered:**

| Approach | Pros | Cons |
|----------|------|------|
| **REST API** ✅ | Official, stable, well-documented, works over HTTP, no filesystem access needed | Requires consumer key/secret, rate limits on large stores |
| WP-CLI | Direct, powerful | Requires exec into the WordPress container or a sidecar, brittle coupling |
| Direct DB queries | Fastest | Bypasses WooCommerce hooks/validation, schema is undocumented and changes between versions |

**Justification:** The REST API is the only approach that works without filesystem or process-level access to the WordPress pod. It is the officially supported integration path, has versioned endpoints (v3), and consumer key/secret auth is designed exactly for server-to-server use. The woo-mcp server stores credentials via Kubernetes Secrets and never exposes them beyond the pod.

### Credential flow

```
Kubernetes Secret (woo-consumer-key, woo-consumer-secret)
    → mounted as env vars in woo-mcp pod
    → used in Authorization header to WooCommerce REST API
    → never forwarded to gateway or browser
```

---

## 5. Authentication and Authorization

### Two auth surfaces

1. **Dashboard login (humans):** Session-based auth with HTTP-only cookies. Users log in with email + password. Passwords are hashed with bcrypt (cost=12). Sessions are stored in PostgreSQL with a TTL.

2. **Gateway API keys (programmatic):** For external MCP clients and the chat agent. API keys follow the format:

   ```
   tb_<prefix>_<random>
   ```

   - `tb_` identifies it as a Tollbooth key.
   - `<prefix>` is a random 8-character hex string stored in plaintext for **lookup** (like a username).
   - `<random>` is 32 bytes of `crypto/rand` output, base64url-encoded. This part is **hashed with SHA-256** before storage.
   - On authentication: extract prefix → query DB → hash the provided key → compare with stored hash.

   **Why SHA-256 and not bcrypt for API keys?** API keys are high-entropy random strings (not human-chosen passwords), so brute-force is infeasible. SHA-256 is O(1) per check vs bcrypt's intentional slowness, which matters for every API call.

### Key revocation

- Keys have a `revoked_at` timestamp. Setting it immediately invalidates the key.
- The `last_used_at` column helps admins identify stale keys.
- Revoked keys are soft-deleted (kept for audit trail) and periodically purged.

### Roles

| Role | Capabilities |
|------|-------------|
| **admin** | Full access: manage users, manage servers, manage API keys, view all audit logs, use chat |
| **member** | Use chat, view tools, view own audit logs, manage own API keys |

### Chat agent auth

The chat agent in the dashboard calls tools through the gateway **as the logged-in user**. The dashboard backend issues a short-lived (5 min) API key scoped to the user's session and role. This key is used for the MCP connection and never exposed to the browser — the dashboard backend proxies tool calls.

---

## 6. Credential Handling for Upstream Secrets

### Storage

Upstream MCP server credentials (e.g., WooCommerce consumer keys) are stored in the `mcp_servers` table in PostgreSQL.

### Encryption at rest

Credentials are encrypted before storage using AES-256-GCM:

1. A 256-bit **data encryption key (DEK)** is generated per credential and stored alongside the ciphertext.
2. The DEK itself is encrypted with a **key encryption key (KEK)** that lives in a Kubernetes Secret (`tollbooth-kek`).
3. This is envelope encryption: rotating the KEK only requires re-encrypting the DEKs, not all credentials.

```
KEK (in K8s Secret) → encrypts → DEK (in PostgreSQL alongside ciphertext) → encrypts → credential
```

### Access control

- Upstream credentials are **never** sent to the browser or to MCP clients.
- The gateway decrypts credentials only in-memory, only when establishing a connection to an upstream.
- The `GET /api/servers` endpoint returns server metadata but **never** the credential field.

---

## 7. Cluster Bootstrap, Networking and NetworkPolicy

### CNI Selection: KinD kindnet vs. Calico/Cilium
In KinD v0.25, `kindnetd` implements native Kubernetes `NetworkPolicy` enforcement using kernel packet filtering (nftables/iptables). We opted to use native `kindnet` rather than disabling default CNI to install Calico or Cilium:
- **Calico/Cilium Overhead:** Installing Calico in local dev KinD containers requires daemonsets (Felix, BIRD, Typha) consuming 500MB+ RAM per node and often causes startup race conditions during container node initialization.
- **kindnet Stability:** KinD v0.25's kindnet natively enforces Layer 3/4 network policies with near-zero overhead.
- **Empirical Proof:** Verified via `deploy/scripts/test-netpol.sh`. The test establishes a throwaway namespace with a default-deny ingress policy, confirms traffic from an unauthorized curl pod is actively dropped (exit code 28: connection timeout), applies an ingress allow policy for labeled pods, and confirms immediate HTTP 200 connectivity.

### Ingress & Local Domain Approach
Local ingress uses wildcard DNS `*.127.0.0.1.nip.io` (or `localtest.me`), mapping directly to `127.0.0.1` without local `/etc/hosts` modifications:
- `dashboard.127.0.0.1.nip.io` → React Dashboard
- `api.127.0.0.1.nip.io` → Gateway REST API & MCP endpoint
- `store.127.0.0.1.nip.io` → WordPress Storefront & Admin

For offline demonstrations, add the following to `/etc/hosts`:
```
127.0.0.1 store.127.0.0.1.nip.io dashboard.127.0.0.1.nip.io api.127.0.0.1.nip.io
```

### Local Image Loading in KinD
Custom images built on the host (`docker build`) can be loaded directly into KinD nodes using `kind load docker-image <image:tag> --name tollbooth`. All Helm workloads declare `imagePullPolicy: IfNotPresent`, ensuring cached local images are used without network roundtrips, while standard upstream images (`wordpress`, `mariadb`, `postgres`) are pulled automatically if not cached.

---

## 8. Store Layer Architecture (WordPress + WooCommerce + MariaDB)

### Workload Specifications
- **MariaDB:** Deployed as a `StatefulSet` with `volumeClaimTemplates` to ensure persistent storage semantics. Uses official `mariadb:11.4` (LTS). Health probes execute `/usr/local/bin/healthcheck.sh --connect --innodb_initialized`.
- **WordPress:** Deployed as a `Deployment` with a persistent volume claim mounted at `/var/www/html/wp-content`. Uses official `wordpress:6.7-php8.2-apache`. Health probes query `/wp-login.php`.
- **Ingress Isolation:** Only the WordPress storefront/admin (`store.127.0.0.1.nip.io`) is exposed via Ingress. MariaDB is strictly exposed via a `ClusterIP` service on port 3306, inaccessible from outside the cluster.

### Security Contexts & Privilege Justification
- **MariaDB:** Runs fully unprivileged with `securityContext`: `runAsNonRoot: true`, `runAsUser: 999`, `fsGroup: 999`.
- **WordPress:** Runs Apache as root initially because the Linux kernel requires root privileges (`CAP_NET_BIND_SERVICE`) to bind standard HTTP port 80 and initialize log files in `/var/log/apache2`. Once started, Apache immediately drops privileges to the unprivileged `www-data` user (UID 33) for all worker processes handling incoming HTTP requests.

---

## 9. WooCommerce REST API Authentication & Key Storage

### How WooCommerce Hashes and Stores API Keys
WooCommerce does not store consumer keys in plaintext. Inspecting `includes/wc-core-functions.php` and `includes/class-wc-rest-authentication.php` reveals the exact hashing implementation:
```php
function wc_api_hash( $data ) {
    return hash_hmac( 'sha256', $data, 'wc-api' );
}
```
When an API key is generated:
1. `consumer_key` is generated (prefixed `ck_...`).
2. `consumer_secret` is generated (prefixed `cs_...`).
3. The row is inserted into `wp_woocommerce_api_keys`:
   - `consumer_key`: The 64-character hex string of `hash_hmac('sha256', $consumer_key, 'wc-api')`.
   - `consumer_secret`: Stored in plaintext (prefixed `cs_...`).
   - `truncated_key`: The last 7 characters of `$consumer_key`.
   - `permissions`: `'read_write'`.
   - `user_id`: Tied to a dedicated shop-manager user (`shop_manager`).

### In-Cluster HTTP Authentication: The `is_ssl()` Requirement
In WooCommerce core (`class-wc-rest-authentication.php`):
```php
public function authenticate( $user_id ) {
    if ( ! empty( $user_id ) || ! $this->is_request_to_rest_api() ) {
        return $user_id;
    }
    if ( is_ssl() ) {
        $user_id = $this->perform_basic_authentication();
    }
    if ( $user_id ) {
        return $user_id;
    }
    return $this->perform_oauth_authentication();
}
```
- **The Problem:** Over plain HTTP (such as internal Kubernetes ClusterIP communication `http://store-wordpress:80/wp-json/wc/v3/...`), `is_ssl()` evaluates to `false`. When `is_ssl()` is false, WooCommerce **refuses** to evaluate Basic Authentication headers or query parameters, failing with `401 Unauthorized` (`woocommerce_rest_cannot_view: Sorry, you cannot list resources`) and falling back to OAuth 1.0a signature verification.
- **The Solution:** Our WordPress deployment sets `WORDPRESS_CONFIG_EXTRA` in `wp-config.php`:
  ```php
  if (isset($_SERVER['HTTP_X_FORWARDED_PROTO']) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https') {
      $_SERVER['HTTPS'] = 'on';
  }
  ```
- **How woo-mcp Talks to the Store:** When `woo-mcp` calls the WooCommerce REST API, it sends:
  1. Standard HTTP Basic Auth: `Authorization: Basic base64(consumer_key:consumer_secret)`
  2. Header: `X-Forwarded-Proto: https`
  This satisfies `is_ssl()` within WordPress, allowing clean, fast, header-based authentication without OAuth 1.0a signing overhead.

---

## 10. Kubernetes Layout

### Namespaces

| Namespace | Contents |
|-----------|----------|
| `tollbooth` | All application workloads (gateway, woo-mcp, dashboard, WordPress, MariaDB, PostgreSQL) |
| `ingress-nginx` | Ingress controller (managed separately) |

Single namespace simplifies RBAC and service discovery for this project scope. Services reference each other by short DNS names (e.g., `gateway`, `postgres`).

### Secrets

| Secret Name | Namespace | Contents |
|-------------|-----------|----------|
| `postgres-credentials` | tollbooth | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| `mariadb-credentials` | tollbooth | `MYSQL_ROOT_PASSWORD`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_DATABASE` |
| `woo-credentials` | tollbooth | `WOO_CONSUMER_KEY`, `WOO_CONSUMER_SECRET` |
| `tollbooth-kek` | tollbooth | `KEK` (256-bit key for envelope encryption) |
| `gateway-session-secret` | tollbooth | `SESSION_SECRET` (for cookie signing) |

All secrets are created via `helm install --set` or `kubectl create secret` — never committed to the repo.

### PVCs

| PVC | Size (local/prod) | Used by |
|-----|-------------------|---------|
| `postgres-data` | 1Gi / 10Gi | PostgreSQL |
| `mariadb-data` | 1Gi / 10Gi | MariaDB |
| `wp-content` | 1Gi / 5Gi | WordPress (plugins, uploads) |

### Services and exposure

| Service | Type | Port | Accessible from |
|---------|------|------|-----------------|
| `gateway` | ClusterIP | 8080 | Ingress, other pods |
| `dashboard` | ClusterIP | 8080 | Ingress |
| `woo-mcp` | ClusterIP | 8081 | Gateway pod only |
| `wordpress` | ClusterIP | 80 | woo-mcp, Ingress (local only for setup) |
| `mariadb` | ClusterIP | 3306 | WordPress pod only |
| `postgres` | ClusterIP | 5432 | Gateway pod only |

### NetworkPolicy (planned for Chunk 2+)

```
woo-mcp:    ingress only from gateway
mariadb:    ingress only from wordpress
postgres:   ingress only from gateway
wordpress:  ingress only from woo-mcp and ingress-nginx
```

---

## Open Questions

1. **Session store:** Should we use PostgreSQL for session storage or switch to Redis? PostgreSQL is simpler (fewer moving parts) but Redis handles TTL natively. Current decision: PostgreSQL, revisit if session volume becomes a concern.

2. **Multi-tenancy:** The current design has one gateway instance managing multiple upstream servers. If we need per-team isolation, we'd need to shard by namespace or add tenant IDs. Current decision: single-tenant, note in THREAT_MODEL.md.

3. **Rate limiting:** Not currently planned. Should we add per-user rate limits on tool calls? Defer to a later chunk if needed.

4. **Webhook support:** WooCommerce supports webhooks for real-time updates. Should woo-mcp expose a webhook receiver so tools can return fresher data? Defer to post-MVP.

5. **Key rotation automation:** The KEK rotation process is manual (re-encrypt DEKs). Should we automate this with a CronJob? Defer.
