-- 0148 was added after some databases had already passed that migration
-- number. Restore the case creator column for those existing installations.
ALTER TABLE ai_investigations
    ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_ai_investigations_created_by
    ON ai_investigations (tenant_id, created_by, created_at DESC);
