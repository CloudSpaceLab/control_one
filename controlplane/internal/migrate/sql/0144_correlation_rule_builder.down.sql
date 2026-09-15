ALTER TABLE correlation_rules
    DROP COLUMN IF EXISTS suppression_seconds,
    DROP COLUMN IF EXISTS group_by,
    DROP COLUMN IF EXISTS event_type;
