DROP TABLE IF EXISTS notifications;
DROP INDEX IF EXISTS idx_ai_investigations_assignee;
ALTER TABLE ai_investigations
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS assigned_by,
    DROP COLUMN IF EXISTS assigned_at;
