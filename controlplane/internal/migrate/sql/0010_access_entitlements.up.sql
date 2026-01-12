CREATE TABLE IF NOT EXISTS access_entitlements (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    node_id UUID REFERENCES nodes(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    group_name TEXT,
    role TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS access_syncs (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    sync_type TEXT NOT NULL,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sync_status TEXT NOT NULL,
    sync_error TEXT,
    users_synced INTEGER DEFAULT 0,
    groups_synced INTEGER DEFAULT 0,
    entitlements_synced INTEGER DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS access_entitlements_tenant_id_idx ON access_entitlements(tenant_id);
CREATE INDEX IF NOT EXISTS access_entitlements_node_id_idx ON access_entitlements(node_id);
CREATE INDEX IF NOT EXISTS access_entitlements_user_id_idx ON access_entitlements(user_id);
CREATE INDEX IF NOT EXISTS access_entitlements_expires_at_idx ON access_entitlements(expires_at);
CREATE INDEX IF NOT EXISTS access_entitlements_revoked_at_idx ON access_entitlements(revoked_at);

CREATE INDEX IF NOT EXISTS access_syncs_tenant_id_idx ON access_syncs(tenant_id);
CREATE INDEX IF NOT EXISTS access_syncs_node_id_idx ON access_syncs(node_id);
CREATE INDEX IF NOT EXISTS access_syncs_synced_at_idx ON access_syncs(synced_at);
CREATE INDEX IF NOT EXISTS access_syncs_status_idx ON access_syncs(sync_status);



