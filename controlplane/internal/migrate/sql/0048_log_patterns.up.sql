CREATE TABLE IF NOT EXISTS log_patterns (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID REFERENCES nodes(id) ON DELETE CASCADE,
    source        TEXT NOT NULL,
    pattern       TEXT NOT NULL,
    hits          INTEGER NOT NULL DEFAULT 1,
    first_seen    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_log_patterns_tenant ON log_patterns (tenant_id);
CREATE INDEX IF NOT EXISTS idx_log_patterns_last   ON log_patterns (last_seen DESC);
