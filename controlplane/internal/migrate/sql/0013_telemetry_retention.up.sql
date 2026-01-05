-- Telemetry retention policies
CREATE TABLE IF NOT EXISTS telemetry_retention_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID,
    policy_name VARCHAR(255) NOT NULL,
    data_type VARCHAR(50) NOT NULL, -- 'metrics', 'logs', 'both'
    retention_days INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, policy_name)
);

CREATE INDEX idx_telemetry_retention_policies_tenant ON telemetry_retention_policies(tenant_id);
CREATE INDEX idx_telemetry_retention_policies_data_type ON telemetry_retention_policies(data_type);
CREATE INDEX idx_telemetry_retention_policies_enabled ON telemetry_retention_policies(enabled);

COMMENT ON TABLE telemetry_retention_policies IS 'Defines retention policies for telemetry data';
COMMENT ON COLUMN telemetry_retention_policies.tenant_id IS 'Tenant ID (NULL for global policy)';
COMMENT ON COLUMN telemetry_retention_policies.policy_name IS 'Unique policy name';
COMMENT ON COLUMN telemetry_retention_policies.data_type IS 'Type of data: metrics, logs, or both';
COMMENT ON COLUMN telemetry_retention_policies.retention_days IS 'Number of days to retain data';


