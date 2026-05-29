CREATE TABLE IF NOT EXISTS private_access_provider_snapshots (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL CHECK (provider IN ('netbird', 'headscale', 'openziti')),
    account_id   TEXT NOT NULL DEFAULT '',
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snapshot     JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, account_id)
);

CREATE INDEX IF NOT EXISTS idx_private_access_snapshots_tenant_updated
    ON private_access_provider_snapshots (tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS private_access_exposure_findings (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider     TEXT CHECK (provider IN ('netbird', 'headscale', 'openziti')),
    finding_type TEXT NOT NULL,
    severity     TEXT NOT NULL,
    node_id      UUID REFERENCES nodes(id) ON DELETE SET NULL,
    service_id   UUID REFERENCES node_services(id) ON DELETE SET NULL,
    detail       TEXT NOT NULL DEFAULT '',
    evidence     JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_private_access_findings_tenant_open
    ON private_access_exposure_findings (tenant_id, resolved_at, severity, finding_type);

CREATE INDEX IF NOT EXISTS idx_private_access_findings_node
    ON private_access_exposure_findings (node_id, resolved_at);
