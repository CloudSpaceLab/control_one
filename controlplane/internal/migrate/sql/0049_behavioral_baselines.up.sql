CREATE TABLE IF NOT EXISTS behavioral_baselines (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID REFERENCES nodes(id) ON DELETE CASCADE,
    signal_type   TEXT NOT NULL,
    dimension     TEXT NOT NULL,
    baseline      JSONB NOT NULL DEFAULT '{}'::jsonb,
    window_days   INTEGER NOT NULL DEFAULT 30,
    computed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, node_id, signal_type, dimension)
);

CREATE INDEX IF NOT EXISTS idx_baselines_tenant_signal ON behavioral_baselines (tenant_id, signal_type);
CREATE INDEX IF NOT EXISTS idx_baselines_node          ON behavioral_baselines (node_id);
