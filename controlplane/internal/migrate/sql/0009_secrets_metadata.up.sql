CREATE TABLE IF NOT EXISTS secret_groups (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    backend TEXT NOT NULL,
    endpoint TEXT,
    sync_interval_seconds INTEGER,
    last_sync_at TIMESTAMPTZ,
    sync_status TEXT,
    sync_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS secret_syncs (
    id UUID PRIMARY KEY,
    secret_group_id UUID NOT NULL REFERENCES secret_groups(id) ON DELETE CASCADE,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    secret_path TEXT NOT NULL,
    secret_version TEXT,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sync_status TEXT NOT NULL,
    sync_error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS secret_groups_tenant_id_idx ON secret_groups(tenant_id);
CREATE INDEX IF NOT EXISTS secret_groups_name_idx ON secret_groups(name);

CREATE INDEX IF NOT EXISTS secret_syncs_secret_group_id_idx ON secret_syncs(secret_group_id);
CREATE INDEX IF NOT EXISTS secret_syncs_node_id_idx ON secret_syncs(node_id);
CREATE INDEX IF NOT EXISTS secret_syncs_synced_at_idx ON secret_syncs(synced_at);
CREATE INDEX IF NOT EXISTS secret_syncs_status_idx ON secret_syncs(sync_status);



