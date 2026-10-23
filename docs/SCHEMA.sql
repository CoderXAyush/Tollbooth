-- Tollbooth Gateway – PostgreSQL Schema
-- ────────────────────────────────────────────────────────────
-- This schema is for the gateway's PostgreSQL database.
-- MariaDB (WordPress/WooCommerce) has its own schema managed by WordPress.

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ════════════════════════════════════════════════════════════
-- USERS
-- ════════════════════════════════════════════════════════════
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,            -- bcrypt, cost=12
    role          TEXT NOT NULL DEFAULT 'member'
                  CHECK (role IN ('admin', 'member')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE INDEX idx_users_email ON users (email);

-- ════════════════════════════════════════════════════════════
-- SESSIONS (dashboard login sessions)
-- ════════════════════════════════════════════════════════════
CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,               -- SHA-256 of session token
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT sessions_token_hash_unique UNIQUE (token_hash)
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- ════════════════════════════════════════════════════════════
-- API KEYS
-- ════════════════════════════════════════════════════════════
-- Format: tb_<prefix>_<secret>
-- - prefix: 8 hex chars, stored in plaintext for lookup
-- - secret: 32 random bytes base64url-encoded, stored as SHA-256 hash
CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,              -- user-defined label
    prefix      TEXT NOT NULL,              -- 8 hex chars, used for lookup
    key_hash    TEXT NOT NULL,              -- SHA-256 of the full key
    revoked_at  TIMESTAMPTZ,               -- NULL = active, set = revoked
    last_used_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT api_keys_prefix_unique UNIQUE (prefix)
);

CREATE INDEX idx_api_keys_user_id ON api_keys (user_id);
CREATE INDEX idx_api_keys_prefix ON api_keys (prefix) WHERE revoked_at IS NULL;

-- ════════════════════════════════════════════════════════════
-- MCP SERVERS (upstream registrations)
-- ════════════════════════════════════════════════════════════
CREATE TABLE mcp_servers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug              TEXT NOT NULL,
    name              TEXT NOT NULL,
    url               TEXT NOT NULL,         -- MCP endpoint URL
    enabled           BOOLEAN NOT NULL DEFAULT true,

    -- Encrypted credential (AES-256-GCM envelope encryption)
    -- NULL means no auth needed for this upstream
    credential_enc    BYTEA,                 -- encrypted credential blob
    credential_nonce  BYTEA,                 -- GCM nonce (12 bytes)
    credential_dek    BYTEA,                 -- encrypted DEK

    -- Health tracking
    status            TEXT NOT NULL DEFAULT 'unknown'
                      CHECK (status IN ('connected', 'unreachable', 'disabled', 'unknown')),
    last_error        TEXT,
    last_checked_at   TIMESTAMPTZ,
    consecutive_fails INT NOT NULL DEFAULT 0,

    -- Timeouts (per-server override)
    timeout_ms        INT NOT NULL DEFAULT 30000,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT mcp_servers_slug_unique UNIQUE (slug),
    CONSTRAINT mcp_servers_slug_format CHECK (slug ~ '^[a-z][a-z0-9_]{0,62}$')
);

CREATE INDEX idx_mcp_servers_slug ON mcp_servers (slug);
CREATE INDEX idx_mcp_servers_enabled ON mcp_servers (enabled) WHERE enabled = true;

-- ════════════════════════════════════════════════════════════
-- AUDIT LOG
-- ════════════════════════════════════════════════════════════
-- Every tool call through the gateway is logged here.
-- This table is append-only in application code.
CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    server_id   UUID REFERENCES mcp_servers(id) ON DELETE SET NULL,
    server_slug TEXT NOT NULL,              -- denormalized for query after server deletion
    tool        TEXT NOT NULL,              -- prefixed tool name (e.g., woo_store1__list_products)
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INT NOT NULL,
    outcome     TEXT NOT NULL CHECK (outcome IN ('success', 'error', 'timeout')),
    error       TEXT,                       -- error message if outcome != 'success'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Query patterns: filter by user, server, tool, time range, outcome
CREATE INDEX idx_audit_log_user_id ON audit_log (user_id);
CREATE INDEX idx_audit_log_server_slug ON audit_log (server_slug);
CREATE INDEX idx_audit_log_tool ON audit_log (tool);
CREATE INDEX idx_audit_log_started_at ON audit_log (started_at DESC);
CREATE INDEX idx_audit_log_outcome ON audit_log (outcome);

-- Composite index for common dashboard query: recent logs by user
CREATE INDEX idx_audit_log_user_time ON audit_log (user_id, started_at DESC);

-- ════════════════════════════════════════════════════════════
-- CHAT SESSIONS
-- ════════════════════════════════════════════════════════════
CREATE TABLE chat_sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL DEFAULT 'New Chat',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_sessions_user_id ON chat_sessions (user_id);

-- ════════════════════════════════════════════════════════════
-- CHAT MESSAGES
-- ════════════════════════════════════════════════════════════
CREATE TABLE chat_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content         TEXT NOT NULL,
    -- For tool messages: which tool was called and what it returned
    tool_call_id    TEXT,
    tool_name       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_messages_session_id ON chat_messages (session_id, created_at ASC);
