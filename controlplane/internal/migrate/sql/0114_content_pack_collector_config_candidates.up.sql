-- Rendered SIEM edge-collector configs waiting for approval/apply.
--
-- These rows intentionally do not mean a collector is running the config. They
-- preserve the exact rendered YAML and config_version so approval, deployment,
-- rollback, and future collector heartbeat evidence can all reference the same
-- artifact.

-- Compatibility note: older production builds used migration 0113 for node
-- service application inventory. Keep this dependency creation here as well
-- so upgraded databases that already recorded version 0113 still get the
-- registry table before this migration adds the foreign key below.
CREATE TABLE IF NOT EXISTS content_pack_registry_snapshots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status              TEXT        NOT NULL DEFAULT 'active'
                                        CHECK (status IN ('active', 'superseded')),
    source              TEXT        NOT NULL DEFAULT '',
    control_one_version TEXT        NOT NULL DEFAULT '',
    pack_count          INTEGER     NOT NULL DEFAULT 0 CHECK (pack_count >= 0),
    snapshot            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_pack_registry_snapshots_active
    ON content_pack_registry_snapshots (tenant_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_content_pack_registry_snapshots_tenant_created
    ON content_pack_registry_snapshots (tenant_id, created_at DESC);

COMMENT ON TABLE content_pack_registry_snapshots IS 'Tenant-scoped snapshots of Control One SIEM content-pack registry lifecycle state';

CREATE TABLE IF NOT EXISTS content_pack_collector_config_candidates (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    registry_snapshot_id UUID        REFERENCES content_pack_registry_snapshots(id) ON DELETE SET NULL,
    status               TEXT        NOT NULL DEFAULT 'rendered'
                                       CHECK (status IN ('rendered', 'approved', 'queued', 'deployed', 'superseded', 'failed', 'rolled_back')),
    config_version       TEXT        NOT NULL,
    collector_id         TEXT        NOT NULL DEFAULT '',
    endpoint             TEXT        NOT NULL DEFAULT '',
    source_ids           TEXT[]      NOT NULL DEFAULT ARRAY[]::TEXT[],
    plan                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rendered_yaml        TEXT        NOT NULL DEFAULT '',
    created_by_subject   TEXT        NOT NULL DEFAULT '',
    approved_by_subject  TEXT        NOT NULL DEFAULT '',
    approval_note        TEXT        NOT NULL DEFAULT '',
    reviewed_config_version TEXT      NOT NULL DEFAULT '',
    reviewed_yaml_sha256 TEXT         NOT NULL DEFAULT '',
    approved_at          TIMESTAMPTZ,
    queued_by_subject    TEXT        NOT NULL DEFAULT '',
    queue_note           TEXT        NOT NULL DEFAULT '',
    target_collector_id  TEXT        NOT NULL DEFAULT '',
    queued_at            TIMESTAMPTZ,
    deployed_at          TIMESTAMPTZ,
    failed_at            TIMESTAMPTZ,
    deployment_error     TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_pack_collector_config_candidates_tenant_created
    ON content_pack_collector_config_candidates (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_collector_config_candidates_tenant_version
    ON content_pack_collector_config_candidates (tenant_id, config_version);

CREATE INDEX IF NOT EXISTS idx_content_pack_collector_config_candidates_tenant_status_created
    ON content_pack_collector_config_candidates (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_pack_collector_config_candidates_snapshot
    ON content_pack_collector_config_candidates (registry_snapshot_id);

COMMENT ON TABLE content_pack_collector_config_candidates IS 'Tenant-scoped rendered SIEM edge-collector configs pending approval/deployment';
