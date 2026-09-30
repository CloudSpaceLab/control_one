ALTER TABLE correlation_rules
    DROP COLUMN IF EXISTS response_enforcement,
    DROP COLUMN IF EXISTS response_scope,
    DROP COLUMN IF EXISTS response_ttl_seconds,
    DROP COLUMN IF EXISTS response_mode;
