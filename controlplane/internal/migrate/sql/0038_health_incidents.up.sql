CREATE TABLE IF NOT EXISTS health_incidents (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID REFERENCES nodes(id) ON DELETE SET NULL,
    incident_type TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'medium',
    details       JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedup_key     TEXT,
    opened_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_health_tenant_opened ON health_incidents (tenant_id, opened_at DESC);
CREATE INDEX IF NOT EXISTS idx_health_node          ON health_incidents (node_id);
CREATE INDEX IF NOT EXISTS idx_health_open          ON health_incidents (tenant_id)
    WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_health_dedup         ON health_incidents (tenant_id, dedup_key)
    WHERE dedup_key IS NOT NULL AND resolved_at IS NULL;
