# Tollbooth – API Reference

## Conventions

### Base URL

| Environment | URL |
|-------------|-----|
| Local (kind) | `http://api.localtest.me` |
| Production | `https://api.yourdomain.com` |

### Authentication

Two auth mechanisms:

1. **Session cookie** (`tb_session`): Set after `POST /api/auth/login`. HTTP-only, Secure (prod), SameSite=Lax.
2. **API key header**: `Authorization: Bearer tb_<prefix>_<secret>`

All endpoints except `/api/auth/login`, `/api/auth/register`, and `/healthz`/`/readyz` require authentication.

### Error Format

All errors return a consistent JSON envelope:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "slug must match ^[a-z][a-z0-9_]{0,62}$",
    "details": {}
  }
}
```

**Error codes:**

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `UNAUTHORIZED` | 401 | Missing or invalid credentials |
| `FORBIDDEN` | 403 | Valid credentials but insufficient role |
| `NOT_FOUND` | 404 | Resource does not exist |
| `CONFLICT` | 409 | Duplicate resource (e.g., slug collision) |
| `VALIDATION_ERROR` | 422 | Request body failed validation |
| `UPSTREAM_ERROR` | 502 | Upstream MCP server returned an error |
| `UPSTREAM_TIMEOUT` | 504 | Upstream MCP server timed out |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

### Pagination

List endpoints support cursor-based pagination:

```
GET /api/audit-logs?cursor=<opaque>&limit=50
```

Response includes:
```json
{
  "data": [...],
  "next_cursor": "eyJpZCI6MTIzfQ==",
  "has_more": true
}
```

---

## Auth Endpoints

### `POST /api/auth/login`

Authenticate a user and set a session cookie.

**Request:**
```json
{
  "email": "admin@example.com",
  "password": "correct-horse-battery-staple"
}
```

**Response (200):**
```json
{
  "user": {
    "id": "uuid",
    "email": "admin@example.com",
    "role": "admin",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

**Headers:** Sets `Set-Cookie: tb_session=<token>; HttpOnly; Path=/; SameSite=Lax`

**Errors:** `401 UNAUTHORIZED` on bad credentials.

### `POST /api/auth/logout`

Clear the session cookie.

**Response (204):** No body.

### `GET /api/auth/me`

Return the currently authenticated user.

**Response (200):**
```json
{
  "user": {
    "id": "uuid",
    "email": "admin@example.com",
    "role": "admin",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

---

## User Management (admin only)

### `GET /api/users`

List all users.

**Response (200):**
```json
{
  "data": [
    {
      "id": "uuid",
      "email": "member@example.com",
      "role": "member",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

### `POST /api/users/invite`

Invite a new user. Sends an invite link (or returns a one-time token in dev mode).

**Request:**
```json
{
  "email": "newmember@example.com",
  "role": "member"
}
```

**Response (201):**
```json
{
  "invite": {
    "id": "uuid",
    "email": "newmember@example.com",
    "role": "member",
    "token": "only-in-dev-mode",
    "expires_at": "2026-01-02T00:00:00Z"
  }
}
```

### `DELETE /api/users/{id}`

Remove a user (admin only, cannot remove self).

**Response (204):** No body.

---

## API Key Management

### `POST /api/keys`

Create a new API key for the authenticated user.

**Request:**
```json
{
  "name": "my-cli-key"
}
```

**Response (201):**
```json
{
  "key": {
    "id": "uuid",
    "name": "my-cli-key",
    "prefix": "a1b2c3d4",
    "secret": "tb_a1b2c3d4_<full-secret>",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

> **NOTE:** The `secret` field is returned **only once** at creation time. It cannot be retrieved later.

### `GET /api/keys`

List API keys for the authenticated user (admins see all keys).

**Response (200):**
```json
{
  "data": [
    {
      "id": "uuid",
      "name": "my-cli-key",
      "prefix": "a1b2c3d4",
      "created_at": "2026-01-01T00:00:00Z",
      "last_used_at": "2026-01-15T12:00:00Z",
      "revoked_at": null
    }
  ]
}
```

### `DELETE /api/keys/{id}`

Revoke an API key. Sets `revoked_at` timestamp.

**Response (204):** No body.

---

## Server Management (admin only)

### `POST /api/servers`

Register a new upstream MCP server.

**Request:**
```json
{
  "slug": "woo_store1",
  "name": "Production WooCommerce Store",
  "url": "http://woo-mcp:8081/mcp",
  "credential": "optional-bearer-token",
  "enabled": true
}
```

**Response (201):**
```json
{
  "server": {
    "id": "uuid",
    "slug": "woo_store1",
    "name": "Production WooCommerce Store",
    "url": "http://woo-mcp:8081/mcp",
    "enabled": true,
    "status": "unknown",
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```

> **NOTE:** The `credential` field is never returned in responses.

### `GET /api/servers`

List all registered upstream servers.

**Response (200):**
```json
{
  "data": [
    {
      "id": "uuid",
      "slug": "woo_store1",
      "name": "Production WooCommerce Store",
      "url": "http://woo-mcp:8081/mcp",
      "enabled": true,
      "status": "connected",
      "last_error": null,
      "last_checked_at": "2026-01-15T12:00:00Z",
      "tool_count": 5
    }
  ]
}
```

### `PUT /api/servers/{id}`

Update server configuration.

**Request:** Same shape as POST, all fields optional.

**Response (200):** Updated server object.

### `DELETE /api/servers/{id}`

Unregister an upstream server.

**Response (204):** No body.

### `POST /api/servers/{id}/test`

Test connection to an upstream server. Performs a `tools/list` call and returns the result.

**Response (200):**
```json
{
  "status": "connected",
  "tools": ["list_products", "get_order"],
  "latency_ms": 142
}
```

**Errors:** `502 UPSTREAM_ERROR` or `504 UPSTREAM_TIMEOUT`.

---

## Tools

### `GET /api/tools`

List all tools from all enabled, non-disabled upstream servers.

**Query params:**
- `server` (optional): filter by server slug

**Response (200):**
```json
{
  "data": [
    {
      "name": "woo_store1__list_products",
      "description": "[woo_store1] List all products in the WooCommerce store",
      "input_schema": { "type": "object", "properties": {} },
      "server_slug": "woo_store1",
      "server_status": "connected",
      "stale": false
    }
  ]
}
```

---

## Audit Log

### `GET /api/audit-logs`

Query audit logs with filters.

**Query params:**
- `user_id` (optional): filter by user
- `server_slug` (optional): filter by upstream server
- `tool` (optional): filter by tool name (prefixed)
- `outcome` (optional): `success` or `error`
- `from` (optional): ISO 8601 start time
- `to` (optional): ISO 8601 end time
- `cursor` (optional): pagination cursor
- `limit` (optional, default 50, max 200): page size

**Response (200):**
```json
{
  "data": [
    {
      "id": "uuid",
      "user_id": "uuid",
      "user_email": "admin@example.com",
      "server_slug": "woo_store1",
      "tool": "woo_store1__list_products",
      "started_at": "2026-01-15T12:00:00Z",
      "duration_ms": 142,
      "outcome": "success",
      "error": null
    }
  ],
  "next_cursor": "eyJpZCI6MTIzfQ==",
  "has_more": true
}
```

**Authorization:** Members see only their own logs. Admins see all logs.

---

## Chat

### `POST /api/chat/sessions`

Create a new chat session.

**Request:**
```json
{
  "title": "Order inquiry"
}
```

**Response (201):**
```json
{
  "session": {
    "id": "uuid",
    "title": "Order inquiry",
    "created_at": "2026-01-15T12:00:00Z"
  }
}
```

### `GET /api/chat/sessions`

List chat sessions for the authenticated user.

### `POST /api/chat/sessions/{id}/messages`

Send a message in a chat session. The backend forwards tool calls to the gateway MCP endpoint on behalf of the user.

**Request:**
```json
{
  "content": "Show me the latest 5 orders"
}
```

**Response (200, streamed via SSE):**
```
data: {"type": "text", "content": "Let me look up the recent orders..."}

data: {"type": "tool_call", "tool": "woo_store1__list_orders", "arguments": {"per_page": 5}}

data: {"type": "tool_result", "tool": "woo_store1__list_orders", "content": "[{...}]"}

data: {"type": "text", "content": "Here are the 5 most recent orders: ..."}

data: [DONE]
```

---

## Gateway MCP Endpoint

### `POST /mcp`

The Streamable HTTP MCP endpoint. This is the single MCP server endpoint that external clients connect to.

**Authentication:** `Authorization: Bearer tb_<prefix>_<secret>`

**Transport:** MCP Streamable HTTP per the MCP specification.

- **Session handling:** The gateway issues a `Mcp-Session-Id` header on initialization. Clients must include it in subsequent requests.
- **Supported methods:** `initialize`, `tools/list`, `tools/call`, `ping`.
- **Tool names:** All tool names are prefixed with `<server_slug>__`.
- **DELETE /mcp:** Terminates the MCP session.

**Example initialization:**

```http
POST /mcp HTTP/1.1
Authorization: Bearer tb_a1b2c3d4_...
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-03-26",
    "capabilities": {},
    "clientInfo": {
      "name": "my-client",
      "version": "1.0.0"
    }
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
Mcp-Session-Id: <uuid>

{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-03-26",
    "capabilities": {
      "tools": { "listChanged": true }
    },
    "serverInfo": {
      "name": "tollbooth-gateway",
      "version": "0.1.0"
    }
  }
}
```
