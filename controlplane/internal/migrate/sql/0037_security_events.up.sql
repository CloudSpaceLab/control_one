CREATE TABLE IF NOT EXISTS security_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id      UUID REFERENCES nodes(id) ON DELETE SET NULL,
    event_type   TEXT NOT NULL,
    severity     TEXT NOT NULL DEFAULT 'medium',
    source       TEXT NOT NULL,
    details      JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedup_key    TEXT,
    fired_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sec_events_tenant_fired ON security_events (tenant_id, fired_at DESC);
CREATE INDEX IF NOT EXISTS idx_sec_events_node         ON security_events (node_id);
CREATE INDEX IF NOT EXISTS idx_sec_events_severity     ON security_events (severity);
CREATE INDEX IF NOT EXISTS idx_sec_events_dedup        ON security_events (tenant_id, dedup_key)
    WHERE dedup_key IS NOT NULL;
