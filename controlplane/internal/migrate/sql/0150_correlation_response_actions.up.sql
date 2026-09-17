ALTER TABLE correlation_rules
    ADD COLUMN IF NOT EXISTS response_mode TEXT NOT NULL DEFAULT 'alert_only',
    ADD COLUMN IF NOT EXISTS response_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    ADD COLUMN IF NOT EXISTS response_scope TEXT NOT NULL DEFAULT 'affected',
    ADD COLUMN IF NOT EXISTS response_enforcement TEXT NOT NULL DEFAULT 'firewall';

ALTER TABLE correlation_rules DROP CONSTRAINT IF EXISTS correlation_rules_response_mode_check;
ALTER TABLE correlation_rules ADD CONSTRAINT correlation_rules_response_mode_check
    CHECK (response_mode IN ('alert_only', 'create_proposal', 'require_approval', 'auto_temporary_block'));
ALTER TABLE correlation_rules DROP CONSTRAINT IF EXISTS correlation_rules_response_ttl_check;
ALTER TABLE correlation_rules ADD CONSTRAINT correlation_rules_response_ttl_check
    CHECK (response_ttl_seconds IN (900, 3600, 86400));
ALTER TABLE correlation_rules DROP CONSTRAINT IF EXISTS correlation_rules_response_scope_check;
ALTER TABLE correlation_rules ADD CONSTRAINT correlation_rules_response_scope_check
    CHECK (response_scope IN ('affected', 'fleet'));
ALTER TABLE correlation_rules DROP CONSTRAINT IF EXISTS correlation_rules_response_enforcement_check;
ALTER TABLE correlation_rules ADD CONSTRAINT correlation_rules_response_enforcement_check
    CHECK (response_enforcement IN ('firewall', 'webserver', 'both'));
