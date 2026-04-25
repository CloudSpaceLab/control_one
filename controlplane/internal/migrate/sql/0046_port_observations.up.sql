CREATE TABLE IF NOT EXISTS port_observations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID REFERENCES nodes(id) ON DELETE CASCADE,
    port          INTEGER NOT NULL,
    protocol      TEXT NOT NULL,
    state         TEXT NOT NULL,
    observed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_port_obs_tenant_time ON port_observations (tenant_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_port_obs_node_port   ON port_observations (node_id, port);
