ALTER TABLE correlation_rules
    ADD COLUMN notification_policy JSONB NOT NULL DEFAULT '{}'::jsonb;
