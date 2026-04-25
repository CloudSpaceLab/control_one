CREATE TABLE IF NOT EXISTS issued_certs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    access_request_id   UUID REFERENCES access_requests(id) ON DELETE SET NULL,
    ca_id               UUID NOT NULL REFERENCES ssh_ca(id) ON DELETE CASCADE,
    subject_user        TEXT NOT NULL,
    principals          TEXT[] NOT NULL DEFAULT '{}',
    serial              BIGINT NOT NULL,
    public_key          TEXT NOT NULL,
    signed_cert         TEXT NOT NULL,
    issued_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    revoked_reason      TEXT
);

CREATE INDEX IF NOT EXISTS idx_issued_certs_tenant  ON issued_certs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_issued_certs_request ON issued_certs (access_request_id);
CREATE INDEX IF NOT EXISTS idx_issued_certs_expires ON issued_certs (expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_issued_certs_serial ON issued_certs (ca_id, serial);
