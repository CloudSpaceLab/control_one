ALTER TABLE tenant_event_filters
    ADD COLUMN IF NOT EXISTS connector_auto_connect_medium_risk BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS connector_auto_connect_high_risk BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS connector_auto_connect_programs TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS connector_approval_required_programs TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS connector_blocked_programs TEXT[] NOT NULL DEFAULT '{}';
