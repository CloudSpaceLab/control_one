ALTER TABLE smtp_settings
    ADD COLUMN IF NOT EXISTS recipients TEXT[] NOT NULL DEFAULT '{}';
