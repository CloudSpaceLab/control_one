ALTER TABLE correlation_rules
    ADD COLUMN IF NOT EXISTS condition_groups JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS distinct_field TEXT NOT NULL DEFAULT '';
