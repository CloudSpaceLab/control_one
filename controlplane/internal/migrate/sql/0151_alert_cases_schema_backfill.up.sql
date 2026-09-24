-- 0138 was introduced after databases had already advanced past that version.
-- Repeat its idempotent schema change at a newer version so existing installs
-- receive the alert-to-case link as well as fresh installations.
ALTER TABLE ai_investigations ADD COLUMN IF NOT EXISTS alert_id UUID REFERENCES alerts(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_ai_investigations_alert ON ai_investigations (alert_id);
COMMENT ON COLUMN ai_investigations.alert_id IS 'Optional originating alert when a case is promoted from an alert';
