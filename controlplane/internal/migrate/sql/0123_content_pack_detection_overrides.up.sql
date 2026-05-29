CREATE TABLE IF NOT EXISTS content_pack_detection_overrides (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID NOT NULL,
    pack_id            TEXT NOT NULL,
    pack_version       TEXT NOT NULL,
    source_id          TEXT NOT NULL DEFAULT '',
    detection_id       TEXT NOT NULL,
    state              TEXT NOT NULL CHECK (state IN ('enabled','disabled','suppressed')),
    suppress_until     TIMESTAMPTZ,
    reason             TEXT NOT NULL DEFAULT '',
    created_by_subject TEXT NOT NULL DEFAULT '',
    updated_by_subject TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, pack_id, pack_version, source_id, detection_id)
);

CREATE INDEX IF NOT EXISTS idx_content_pack_detection_overrides_tenant_state
    ON content_pack_detection_overrides (tenant_id, state, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_detection_overrides_detection
    ON content_pack_detection_overrides (tenant_id, pack_id, pack_version, source_id, detection_id);
