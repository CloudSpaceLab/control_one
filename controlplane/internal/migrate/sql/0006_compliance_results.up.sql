CREATE TABLE IF NOT EXISTS compliance_results (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    scan_id TEXT,
    rule_id TEXT NOT NULL,
    passed BOOLEAN NOT NULL,
    severity TEXT,
    details TEXT,
    remediation TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS compliance_results_job_id_idx ON compliance_results(job_id);
CREATE INDEX IF NOT EXISTS compliance_results_tenant_id_idx ON compliance_results(tenant_id);
CREATE INDEX IF NOT EXISTS compliance_results_node_id_idx ON compliance_results(node_id);
CREATE UNIQUE INDEX IF NOT EXISTS compliance_results_job_rule_idx ON compliance_results(job_id, rule_id);
