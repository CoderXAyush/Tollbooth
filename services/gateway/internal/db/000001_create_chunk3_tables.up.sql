-- Tollbooth Gateway – Chunk 3 Core Tables
-- Tables needed for Chunk 3: mcp_servers and mcp_tools cache
-- (users, api_keys, sessions, audit_log deferred to Chunk 4)

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS mcp_servers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug              TEXT NOT NULL,
    name              TEXT NOT NULL,
    url               TEXT NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT true,

    -- Encrypted credential (AES-256-GCM)
    credential_enc    BYTEA,
    credential_nonce  BYTEA,

    -- Health state machine (connected, unreachable, disabled, unknown)
    status            TEXT NOT NULL DEFAULT 'unknown'
                      CHECK (status IN ('connected', 'unreachable', 'disabled', 'unknown')),
    last_error        TEXT,
    last_checked_at   TIMESTAMPTZ,
    consecutive_fails INT NOT NULL DEFAULT 0,

    -- Timeouts
    timeout_ms        INT NOT NULL DEFAULT 30000,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT mcp_servers_slug_unique UNIQUE (slug),
    CONSTRAINT mcp_servers_slug_format CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,18}[a-z0-9])?$')
);

CREATE INDEX IF NOT EXISTS idx_mcp_servers_slug ON mcp_servers (slug);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_enabled ON mcp_servers (enabled) WHERE enabled = true;

CREATE TABLE IF NOT EXISTS mcp_tools (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id     UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT,
    input_schema  JSONB NOT NULL DEFAULT '{}'::jsonb,
    annotations   JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT mcp_tools_server_name_unique UNIQUE (server_id, name)
);

CREATE INDEX IF NOT EXISTS idx_mcp_tools_server_id ON mcp_tools (server_id);
