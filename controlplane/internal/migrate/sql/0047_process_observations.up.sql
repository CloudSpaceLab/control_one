CREATE TABLE IF NOT EXISTS process_observations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID REFERENCES nodes(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    command       TEXT,
    user_name     TEXT,
    observed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_proc_obs_tenant_time ON process_observations (tenant_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_proc_obs_node_name   ON process_observations (node_id, name);
