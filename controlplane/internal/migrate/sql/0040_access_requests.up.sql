CREATE TABLE IF NOT EXISTS access_requests (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id              UUID REFERENCES users(id) ON DELETE SET NULL,
    target_node_id       UUID REFERENCES nodes(id) ON DELETE SET NULL,
    target_resource_type TEXT NOT NULL CHECK (target_resource_type IN ('ssh','rdp','db')),
    requested_access     TEXT NOT NULL,
    justification        TEXT,
    status               TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied','expired','revoked')),
    ttl_seconds          INTEGER NOT NULL DEFAULT 1800 CHECK (ttl_seconds > 0),
    requested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at           TIMESTAMPTZ,
    decided_by           UUID REFERENCES users(id) ON DELETE SET NULL,
    decision_reason      TEXT,
    expires_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_access_req_tenant_status ON access_requests (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_access_req_user          ON access_requests (user_id);
CREATE INDEX IF NOT EXISTS idx_access_req_node          ON access_requests (target_node_id);
CREATE INDEX IF NOT EXISTS idx_access_req_requested    ON access_requests (requested_at DESC);
