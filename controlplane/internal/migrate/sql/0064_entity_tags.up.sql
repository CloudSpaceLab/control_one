-- entity_tags lets analysts attach freeform labels to any investigated
-- entity (ip, process, file, hash, user, host, domain). Used by the
-- SIEM Investigate UI to group / annotate during triage.
CREATE TABLE IF NOT EXISTS entity_tags (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    entity_type  TEXT NOT NULL,
    entity_id    TEXT NOT NULL,
    tag          TEXT NOT NULL,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, entity_type, entity_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_entity_tags_tenant_entity
    ON entity_tags (tenant_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_tags_tag
    ON entity_tags (tenant_id, tag);

-- entity_actions captures operator-driven actions taken from Investigate
-- (block / allow / quarantine). Mirrored to audit_logs by the handler;
-- a dedicated table simplifies per-entity history queries.
CREATE TABLE IF NOT EXISTS entity_actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    entity_type  TEXT NOT NULL,
    entity_id    TEXT NOT NULL,
    action       TEXT NOT NULL CHECK (action IN ('block','allow','quarantine')),
    reason       TEXT,
    ttl_seconds  INTEGER,
    expires_at   TIMESTAMPTZ,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_entity_actions_tenant_entity
    ON entity_actions (tenant_id, entity_type, entity_id, created_at DESC);
