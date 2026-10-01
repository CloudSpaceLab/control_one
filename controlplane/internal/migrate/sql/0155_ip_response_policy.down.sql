ALTER TABLE tenant_remediation_config
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_default_ip_block_ttl_check,
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_default_ip_block_scope_check,
    DROP CONSTRAINT IF EXISTS tenant_remediation_config_auto_block_min_confidence_check,
    DROP COLUMN IF EXISTS require_corroborating_threat_intel,
    DROP COLUMN IF EXISTS default_ip_block_ttl_seconds,
    DROP COLUMN IF EXISTS default_ip_block_scope,
    DROP COLUMN IF EXISTS auto_block_min_confidence,
    DROP COLUMN IF EXISTS auto_block_enabled;
