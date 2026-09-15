ALTER TABLE correlation_rules
    DROP COLUMN IF EXISTS aggregate_threshold,
    DROP COLUMN IF EXISTS aggregate_field,
    DROP COLUMN IF EXISTS sequence_conditions,
    DROP COLUMN IF EXISTS sequence_threshold,
    DROP COLUMN IF EXISTS sequence_event_type;
