ALTER TABLE tenant_event_filters
    DROP COLUMN IF EXISTS connector_blocked_programs,
    DROP COLUMN IF EXISTS connector_approval_required_programs,
    DROP COLUMN IF EXISTS connector_auto_connect_programs,
    DROP COLUMN IF EXISTS connector_auto_connect_high_risk,
    DROP COLUMN IF EXISTS connector_auto_connect_medium_risk;
