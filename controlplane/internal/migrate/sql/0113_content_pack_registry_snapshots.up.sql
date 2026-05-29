-- Durable lifecycle state for SIEM content packs.
--
-- The registry itself owns install/enable/quarantine semantics in Go. This
-- table stores immutable-ish snapshots of that lifecycle state so the control
-- plane can recover enabled/quarantined packs after restart and keep an audit
-- trail of offline content-pack sync operations.

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
