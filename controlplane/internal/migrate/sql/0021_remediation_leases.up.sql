-- Per-node remediation lease to prevent concurrent remediations on the same host.
CREATE TABLE IF NOT EXISTS remediation_leases (
    node_id     UUID PRIMARY KEY REFERENCES nodes(id)   ON DELETE CASCADE,
    tenant_id   UUID NOT NULL    REFERENCES tenants(id) ON DELETE CASCADE,
    job_id      UUID NOT NULL    REFERENCES jobs(id)    ON DELETE CASCADE,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_remediation_leases_tenant  ON remediation_leases (tenant_id);
CREATE INDEX IF NOT EXISTS idx_remediation_leases_expires ON remediation_leases (expires_at);

COMMENT ON TABLE remediation_leases IS 'Serialises remediation jobs to one-in-flight per node';
