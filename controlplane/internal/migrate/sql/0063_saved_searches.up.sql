-- Saved searches power the SIEM Investigate UI: users save common
-- entity / query combinations and optionally share them tenant-wide.
CREATE TABLE IF NOT EXISTS saved_searches (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL,
    owner_user_id  UUID NOT NULL,
    name           TEXT NOT NULL,
    query          TEXT NOT NULL DEFAULT '',
    entity_type    TEXT,
    filters        JSONB NOT NULL DEFAULT '{}'::jsonb,
    shared         BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_saved_searches_tenant_owner
    ON saved_searches (tenant_id, owner_user_id);
CREATE INDEX IF NOT EXISTS idx_saved_searches_tenant_shared
    ON saved_searches (tenant_id, shared);
