DROP INDEX IF EXISTS idx_remediation_circuit_breaker_tripped;
DROP TABLE IF EXISTS remediation_circuit_breaker_state;

DROP INDEX IF EXISTS idx_remediation_approvals_node;
DROP INDEX IF EXISTS idx_remediation_approvals_expires_at;
DROP INDEX IF EXISTS idx_remediation_approvals_tenant_status;
DROP TABLE IF EXISTS remediation_approvals;

DROP TABLE IF EXISTS tenant_remediation_config;
