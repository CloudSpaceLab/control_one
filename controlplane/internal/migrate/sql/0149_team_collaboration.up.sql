-- SOC case collaboration: ownership (assignee) on cases plus a notification
-- inbox for assignments and mentions. Mentions themselves are stored in the
-- existing note audit metadata (soc.case.note.add -> metadata->'mentions').
ALTER TABLE ai_investigations
    ADD COLUMN IF NOT EXISTS assignee_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_at   TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ai_investigations_assignee
    ON ai_investigations (tenant_id, assignee_id, status);

CREATE TABLE IF NOT EXISTS notifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recipient_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('case_assigned', 'case_mentioned')),
    case_id      UUID NOT NULL REFERENCES ai_investigations(id) ON DELETE CASCADE,
    case_title   TEXT NOT NULL DEFAULT '',
    read_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_created
    ON notifications (recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_recipient_unread
    ON notifications (recipient_id, read_at)
    WHERE read_at IS NULL;
