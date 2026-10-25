# Tollbooth – Threat Model

This document identifies security-relevant design decisions and their rationale.

---

## 1. Shared Upstream Credentials

**Threat:** Upstream MCP server credentials (e.g., WooCommerce consumer key/secret) are shared across all users of the gateway. A compromised gateway means all upstream credentials are exposed.

**Mitigations:**
- Credentials are encrypted at rest with AES-256-GCM envelope encryption (see DESIGN.md §6).
- Credentials are decrypted only in-memory when establishing upstream connections.
- The KEK lives in a Kubernetes Secret with RBAC restricting access to the gateway pod's service account.
- Credential values are never included in API responses, logs, or error messages.

**Residual risk:** An attacker with access to the gateway pod's memory can extract decrypted credentials. This is inherent to any application that must use credentials at runtime. Mitigation: pod-level security (non-root, read-only filesystem, no shell in distroless images).

---

## 2. Client Tokens Not Passed to Upstreams

**Threat:** If dashboard session tokens or API keys were forwarded to upstream MCP servers, a malicious upstream could capture and replay them.

**Mitigations:**
- The gateway authenticates clients (dashboard or API key holders) and then makes upstream calls **using its own credentials**. Client tokens never leave the gateway process.
- Upstream servers receive the gateway's bearer token (if configured), not the client's.
- The gateway acts as a **trust boundary**: it authenticates the human/API client, authorizes the action, and then proxies the tool call to the upstream with server-side credentials.

**Residual risk:** None for token forwarding. However, the gateway has full access to all upstreams, so compromising the gateway is equivalent to compromising all upstream credentials (see §1).

---

## 3. Untrusted Upstream Tool Descriptions (Prompt Injection)

**Threat:** Upstream MCP servers provide tool names and descriptions. A malicious or compromised upstream could embed prompt-injection payloads in tool descriptions (e.g., "ignore all previous instructions and..."). When these descriptions are sent to an LLM in the chat agent, the LLM might follow the injected instructions.

**Mitigations:**
- Tool descriptions are treated as untrusted data. The chat agent's system prompt explicitly instructs the LLM to treat tool descriptions as metadata, not instructions.
- Tool descriptions are sanitized: HTML stripped, length capped (1024 chars), control characters removed.
- The gateway prefixes all tool descriptions with `[server_slug]` so the LLM (and humans in the audit log) can identify the source.
- Admin review: admins can see all tool descriptions in the dashboard and disable suspicious servers.

**Residual risk:** Prompt injection is an unsolved problem. Sophisticated injections may still influence LLM behavior despite mitigations. Defense in depth: audit logging records every tool call, and rate limiting (future) can bound the blast radius.

---

## 4. What One Team Member Can Do to Another

**Threat:** In a multi-user gateway, a malicious member could abuse shared tools to affect other members' data or operations.

**Mitigations:**
- **Role-based access control:** Members can only use tools and view their own audit logs. They cannot manage servers, users, or other members' API keys.
- **Audit logging:** Every tool call is logged with the user's identity. Tamper detection: members cannot delete or modify audit logs.
- **API key isolation:** Members can only create and revoke their own API keys. They cannot see other members' keys.
- **No cross-user session access:** Sessions are tied to a single user. There is no session sharing or delegation.

**Residual risk:** If two members both have access to the same upstream tool (e.g., `woo_store1__delete_product`), one member could delete another member's products. This is by design: the upstream (WooCommerce) is a shared resource. Per-resource authorization would need to be implemented at the upstream level, not the gateway. Consider adding tool-level ACLs in a future chunk if needed.

---

## 5. Upstream Service Authentication (woo-mcp Bearer Token)

**Threat:** If the internal `woo-mcp` service did not authenticate callers, any pod in the cluster (or any workload in the same namespace) could call WooCommerce tools directly, bypassing the gateway's audit logging, permission checks, and rate limiting.

**Mitigations:**
- `woo-mcp` requires a static Bearer token (`Authorization: Bearer <token>`) on all `/mcp` endpoints.
- The token is provisioned via Kubernetes Secret (`woo-mcp-auth`) generated with high cryptographic entropy (`openssl rand -base64 32`).
- Validation uses `crypto/subtle.ConstantTimeCompare` to eliminate timing side-channel attacks against token extraction.
- Ingress NetworkPolicy blocks all pods except those explicitly labeled `app.kubernetes.io/name: gateway`.

**Residual risk:** An attacker who compromises the gateway pod can obtain the Bearer token and call `woo-mcp` directly. This is an accepted boundary: the gateway is the designated caller. Pod security standards (non-root, read-only root filesystems, drop all capabilities) mitigate pod takeover.

---

## 6. Plaintext HTTP Basic Auth Inside Kubernetes Cluster

**Threat:** Communication between `woo-mcp` and WordPress uses HTTP Basic Auth (`Authorization: Basic base64(ck:cs)`) and `X-Forwarded-Proto: https` over unencrypted HTTP (`http://store-wordpress:80`). An attacker capable of network packet sniffing on the node or cluster network could capture the WooCommerce consumer key and secret.

**Mitigations:**
- Traffic never leaves the Kubernetes cluster network.
- Pod-to-pod communication is constrained by Kubernetes `NetworkPolicy`:
  - `woo-mcp` egress policy restricts TCP port 80 traffic strictly to pods labeled `app.kubernetes.io/name: wordpress`, `component: store`.
  - WordPress ingress policy allows connections only from `woo-mcp` and the `ingress-nginx` ingress controller.
- In KinD, network traffic flows across local virtual Ethernet pairs (`veth`) and Linux bridge devices managed by `kindnetd` without passing over physical wires.

**Residual risk:** On shared multi-tenant Kubernetes nodes without CNI encryption (e.g., WireGuard or mTLS via Cilium/Linkerd), a privileged user on the node (`root` or `CAP_NET_RAW`) could inspect container network interfaces. For high-security production deployments, transparent mTLS (Cilium WireGuard or Istio ambient mesh) should be enabled.

---

## 7. Unencrypted Database Protocol (`--ssl=0`) During In-Cluster Bootstrap

**Threat:** The Store bootstrap Job uses `mariadb-admin ping ... --ssl=0` and WP-CLI commands that communicate with MariaDB over unencrypted TCP on port 3306 without TLS verification.

**Mitigations:**
- MariaDB is strictly internal (`ClusterIP` only) and never exposed to Ingress or external networks.
- Ingress to MariaDB port 3306 is enforced by `mariadb-netpol`, which permits connections exclusively from pods labeled `app.kubernetes.io/name: wordpress`.
- Passwords are injected via Kubernetes Secrets (`mariadb-credentials`) and never stored in configuration files or scripts.

**Residual risk:** Similar to §6, in-cluster packet inspection by root on the host node could observe SQL traffic. Mitigation for production is enabling MariaDB TLS with internal CA certs.

---

## 8. LLM Context Window Leakage & Customer PII Exposure

**Threat:** LLM systems are vulnerable to prompt extraction, unintentional output leakage, and training data contamination. Exposing customer Personally Identifiable Information (PII) such as home addresses, phone numbers, and full order logs to the model creates privacy risks and potential GDPR/CCPA violations.

**Mitigations:**
- **`list_orders` Stripping:** The order listing tool completely omits customer names, email addresses, phone numbers, and delivery addresses. It returns only order ID, status, order totals, and product line items.
- **`get_order` Minimization:** The order detail tool returns only minimal contact information (`first_name`, `last_name`, `email`) strictly necessary for order identification. Physical shipping/billing street addresses and telephone numbers are completely stripped before returning to the model.
- **HTML Sanitization:** Product descriptions are stripped of raw HTML before serialization to avoid injecting misleading formatting into prompts.
