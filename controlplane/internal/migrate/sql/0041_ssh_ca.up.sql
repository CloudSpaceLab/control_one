CREATE TABLE IF NOT EXISTS ssh_ca (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ca_public_key           TEXT NOT NULL,
    ca_private_key_sealed   BYTEA NOT NULL,
    nonce                   BYTEA NOT NULL,
    key_type                TEXT NOT NULL DEFAULT 'ed25519',
    active                  BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at              TIMESTAMPTZ,
    UNIQUE (tenant_id, active) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX IF NOT EXISTS idx_ssh_ca_tenant ON ssh_ca (tenant_id);
