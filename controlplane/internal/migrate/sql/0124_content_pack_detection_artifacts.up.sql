CREATE TABLE IF NOT EXISTS content_pack_detection_artifacts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL,
    registry_snapshot_id UUID NOT NULL,
    pack_id              TEXT NOT NULL,
    pack_version         TEXT NOT NULL,
    source_id            TEXT NOT NULL,
    detection_id         TEXT NOT NULL,
    detection_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    rule_json            JSONB NOT NULL DEFAULT '{}'::jsonb,
    loaded_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, registry_snapshot_id, pack_id, pack_version, source_id, detection_id)
);

CREATE INDEX IF NOT EXISTS idx_content_pack_detection_artifacts_snapshot
    ON content_pack_detection_artifacts (tenant_id, registry_snapshot_id, source_id, detection_id);
