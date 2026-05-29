-- Tenant-scoped SIEM/observability edge collectors managed or observed by
-- Control One. This records collector presence and health evidence only; config
-- deployment remains a separate approved-candidate/apply workflow.

CREATE TABLE IF NOT EXISTS content_pack_edge_collectors (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    collector_id           TEXT        NOT NULL,
    kind                   TEXT        NOT NULL DEFAULT 'otel'
                                         CHECK (kind IN ('otel', 'alloy', 'fluent_bit', 'vector', 'node_agent')),
    display_name           TEXT        NOT NULL DEFAULT '',
    endpoint               TEXT        NOT NULL DEFAULT '',
    version                TEXT        NOT NULL DEFAULT '',
    status                 TEXT        NOT NULL DEFAULT 'registered'
                                         CHECK (status IN ('registered', 'healthy', 'degraded', 'stale', 'disabled')),
    desired_config_version TEXT        NOT NULL DEFAULT '',
    running_config_version TEXT        NOT NULL DEFAULT '',
    auth_token_hash        TEXT        NOT NULL DEFAULT '',
    token_last_four        TEXT        NOT NULL DEFAULT '',
    token_issued_at        TIMESTAMPTZ,
    health                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    last_error             TEXT        NOT NULL DEFAULT '',
    last_heartbeat_at      TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, collector_id)
);

CREATE INDEX IF NOT EXISTS idx_content_pack_edge_collectors_tenant_status
    ON content_pack_edge_collectors (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_edge_collectors_tenant_heartbeat
    ON content_pack_edge_collectors (tenant_id, last_heartbeat_at DESC NULLS LAST);

COMMENT ON TABLE content_pack_edge_collectors IS 'Tenant-scoped OTel/compatible edge collectors and their latest health evidence';
