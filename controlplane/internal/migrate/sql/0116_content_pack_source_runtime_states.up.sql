-- Durable tenant-scoped SIEM source runtime state.
--
-- Edge collectors can report source/receiver/parser health in heartbeats. This
-- table preserves the latest normalized source state separately from the raw
-- collector heartbeat payload so coverage and UI views can query stable source
-- truth without depending on one transient heartbeat document.

CREATE TABLE IF NOT EXISTS content_pack_source_runtime_states (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    source_instance_id TEXT        NOT NULL,
    source_id          TEXT        NOT NULL DEFAULT '',
    pack_id            TEXT        NOT NULL DEFAULT '',
    pack_version       TEXT        NOT NULL DEFAULT '',
    display_name       TEXT        NOT NULL DEFAULT '',
    node_id            TEXT        NOT NULL DEFAULT '',
    collector_id       TEXT        NOT NULL DEFAULT '',
    collector_mode     TEXT        NOT NULL DEFAULT '',
    parser_id          TEXT        NOT NULL DEFAULT '',
    coverage_state     TEXT        NOT NULL DEFAULT 'discovered'
                                      CHECK (coverage_state IN (
                                          'discovered',
                                          'proposed',
                                          'approval_required',
                                          'approved',
                                          'config_rendered',
                                          'deployed',
                                          'collecting',
                                          'parser_healthy',
                                          'parser_failed',
                                          'silent',
                                          'backpressured',
                                          'unsupported',
                                          'privacy_blocked',
                                          'stale'
                                      )),
    approval_required  BOOLEAN     NOT NULL DEFAULT FALSE,
    approval_id        TEXT        NOT NULL DEFAULT '',
    config_version     TEXT        NOT NULL DEFAULT '',
    content_version    TEXT        NOT NULL DEFAULT '',
    last_event_at      TIMESTAMPTZ,
    last_parsed_at     TIMESTAMPTZ,
    last_health_at     TIMESTAMPTZ,
    last_error         TEXT        NOT NULL DEFAULT '',
    metrics            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    labels             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, source_instance_id)
);

CREATE INDEX IF NOT EXISTS idx_content_pack_source_runtime_tenant_state
    ON content_pack_source_runtime_states (tenant_id, coverage_state, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_source_runtime_tenant_collector
    ON content_pack_source_runtime_states (tenant_id, collector_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_source_runtime_tenant_source
    ON content_pack_source_runtime_states (tenant_id, source_id, updated_at DESC);

COMMENT ON TABLE content_pack_source_runtime_states IS 'Tenant-scoped normalized SIEM source runtime coverage state from edge collectors and future source state writers';
