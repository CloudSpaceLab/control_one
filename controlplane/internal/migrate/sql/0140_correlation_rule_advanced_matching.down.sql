ALTER TABLE correlation_rules
    DROP COLUMN IF EXISTS distinct_field,
    DROP COLUMN IF EXISTS condition_groups;
