CREATE TABLE IF NOT EXISTS hypervisor_hosts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider          TEXT NOT NULL,
    name              TEXT NOT NULL,
    endpoint_url      TEXT NOT NULL,
    credential_id     UUID REFERENCES provider_credentials(id) ON DELETE SET NULL,
    datacenter        TEXT,
    labels            JSONB NOT NULL DEFAULT '{}'::jsonb,
    health_status     TEXT NOT NULL DEFAULT 'unknown',
    health_message    TEXT,
    last_verified_at  TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, name)
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_hosts_tenant ON hypervisor_hosts (tenant_id);
CREATE INDEX IF NOT EXISTS idx_hypervisor_hosts_provider ON hypervisor_hosts (provider);
CREATE INDEX IF NOT EXISTS idx_hypervisor_hosts_credential ON hypervisor_hosts (credential_id);

ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS hypervisor_host_id UUID REFERENCES hypervisor_hosts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_clusters_hypervisor_host ON clusters (hypervisor_host_id);
