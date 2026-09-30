ALTER TABLE ai_investigations
    DROP COLUMN IF EXISTS created_by;

DROP INDEX IF EXISTS idx_ai_investigations_created_by;
DROP INDEX IF EXISTS idx_alerts_acked_resolved;
DROP INDEX IF EXISTS idx_entity_actions_tenant_created;
DROP INDEX IF EXISTS idx_audit_logs_note_add;