CREATE TABLE IF NOT EXISTS offline_content_bundles (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    bundle_id TEXT NOT NULL,
    version TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    public_key_fingerprint TEXT NOT NULL DEFAULT '',
    signature TEXT NOT NULL DEFAULT '',
    manifest_sha256 TEXT NOT NULL DEFAULT '',
    storage_path TEXT NOT NULL DEFAULT '',
    manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    contents JSONB NOT NULL DEFAULT '[]'::jsonb,
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    error TEXT NOT NULL DEFAULT '',
    imported_by UUID REFERENCES users(id) ON DELETE SET NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    issued_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    rollback_to UUID REFERENCES offline_content_bundles(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_offline_content_bundles_tenant_bundle_sequence
    ON offline_content_bundles (tenant_id, bundle_id, sequence);

CREATE UNIQUE INDEX IF NOT EXISTS idx_offline_content_bundles_active
    ON offline_content_bundles (tenant_id, bundle_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_offline_content_bundles_tenant_status
    ON offline_content_bundles (tenant_id, status, imported_at DESC);

CREATE TABLE IF NOT EXISTS offline_content_bundle_audit (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    bundle_row_id UUID REFERENCES offline_content_bundles(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_offline_content_bundle_audit_tenant_created
    ON offline_content_bundle_audit (tenant_id, created_at DESC);
