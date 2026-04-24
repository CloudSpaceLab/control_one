CREATE TABLE IF NOT EXISTS provider_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,
    name            TEXT NOT NULL,
    config_encrypted BYTEA NOT NULL,
    nonce           BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at      TIMESTAMPTZ,
    UNIQUE (tenant_id, provider, name)
);

CREATE INDEX IF NOT EXISTS idx_provider_credentials_tenant ON provider_credentials (tenant_id);
CREATE INDEX IF NOT EXISTS idx_provider_credentials_provider ON provider_credentials (provider);
