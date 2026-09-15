ALTER TABLE smtp_settings
    ADD COLUMN recipients TEXT[] NOT NULL DEFAULT '{}';
