-- Tenant-configurable IP containment policy.
-- Defaults preserve the current production behavior: auto-block only at
-- full confidence, require threat-intel corroboration, affected scope, 1h TTL.

ALTER TABLE tenant_remediation_config
    ADD COLUMN IF NOT EXISTS auto_block_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS auto_block_min_confidence INTEGER NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS default_ip_block_scope TEXT NOT NULL DEFAULT 'affected',
    ADD COLUMN IF NOT EXISTS default_ip_block_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    ADD COLUMN IF NOT EXISTS require_corroborating_threat_intel BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE tenant_remediation_config
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_auto_block_min_confidence_check,
    ADD CONSTRAINT tenant_remediation_config_auto_block_min_confidence_check
        CHECK (auto_block_min_confidence BETWEEN 70 AND 100),
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_default_ip_block_scope_check,
    ADD CONSTRAINT tenant_remediation_config_default_ip_block_scope_check
        CHECK (default_ip_block_scope IN ('affected','fleet')),
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_default_ip_block_ttl_check,
    ADD CONSTRAINT tenant_remediation_config_default_ip_block_ttl_check
        CHECK (default_ip_block_ttl_seconds IN (900, 3600, 86400));

COMMENT ON COLUMN tenant_remediation_config.auto_block_enabled
    IS 'Automatically contain qualifying IP behavior findings when safety gates pass';
COMMENT ON COLUMN tenant_remediation_config.auto_block_min_confidence
    IS 'Minimum IP behavior confidence score required for automatic containment';
COMMENT ON COLUMN tenant_remediation_config.default_ip_block_scope
    IS 'Default manual and automatic IP block scope: affected or fleet';
COMMENT ON COLUMN tenant_remediation_config.default_ip_block_ttl_seconds
    IS 'Default IP block TTL in seconds: 900, 3600, or 86400';
COMMENT ON COLUMN tenant_remediation_config.require_corroborating_threat_intel
    IS 'When true, automatic IP containment requires a positive threat-intel signal';
