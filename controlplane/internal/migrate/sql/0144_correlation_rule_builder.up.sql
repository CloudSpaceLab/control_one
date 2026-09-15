ALTER TABLE correlation_rules
    ADD COLUMN IF NOT EXISTS event_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS group_by TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS suppression_seconds INTEGER NOT NULL DEFAULT 300
        CHECK (suppression_seconds >= 0);

UPDATE correlation_rules
SET group_by = ARRAY[dimension]
WHERE cardinality(group_by) = 0 AND dimension <> '';
