-- Team activity dashboard (CISO visibility). Tags investigations with the
-- analyst who promoted them so per-analyst metrics, trends, and the activity
-- feed can attribute cases without reading the audit trail on every render.
ALTER TABLE ai_investigations
    ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_ai_investigations_created_by
    ON ai_investigations (tenant_id, created_by, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_alerts_acked_resolved
    ON alerts (tenant_id, acked_at, resolved_at);

CREATE INDEX IF NOT EXISTS idx_entity_actions_tenant_created
    ON entity_actions (tenant_id, created_by, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_note_add
    ON audit_logs (tenant_id, action, created_at DESC);