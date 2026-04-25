CREATE TABLE IF NOT EXISTS alerts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_id       UUID,
    node_id       UUID REFERENCES nodes(id) ON DELETE SET NULL,
    source        TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'medium',
    title         TEXT NOT NULL,
    summary       TEXT,
    state         TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','acked','resolved')),
    dedup_key     TEXT,
    context       JSONB NOT NULL DEFAULT '{}'::jsonb,
    opened_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acked_at      TIMESTAMPTZ,
    acked_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at   TIMESTAMPTZ,
    resolved_by   UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_alerts_tenant_state  ON alerts (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_alerts_opened        ON alerts (opened_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_severity      ON alerts (severity);
CREATE INDEX IF NOT EXISTS idx_alerts_node          ON alerts (node_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_open_dedup
    ON alerts (tenant_id, dedup_key)
    WHERE state = 'open' AND dedup_key IS NOT NULL;
