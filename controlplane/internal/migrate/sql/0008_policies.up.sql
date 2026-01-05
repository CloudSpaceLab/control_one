CREATE TABLE IF NOT EXISTS policies (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    description TEXT,
    rule_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS policy_versions (
    id UUID PRIMARY KEY,
    policy_id UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    rule_definition TEXT NOT NULL,
    checksum TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    promoted_at TIMESTAMPTZ,
    UNIQUE (policy_id, version)
);

CREATE TABLE IF NOT EXISTS policy_assignments (
    id UUID PRIMARY KEY,
    policy_id UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    node_id UUID REFERENCES nodes(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by UUID,
    expires_at TIMESTAMPTZ,
    UNIQUE (policy_id, tenant_id, node_id)
);

CREATE INDEX IF NOT EXISTS policies_tenant_id_idx ON policies(tenant_id);
CREATE INDEX IF NOT EXISTS policies_name_idx ON policies(name);
CREATE INDEX IF NOT EXISTS policies_enabled_idx ON policies(enabled);
CREATE INDEX IF NOT EXISTS policies_archived_at_idx ON policies(archived_at);

CREATE INDEX IF NOT EXISTS policy_versions_policy_id_idx ON policy_versions(policy_id);
CREATE INDEX IF NOT EXISTS policy_versions_promoted_at_idx ON policy_versions(promoted_at);

CREATE INDEX IF NOT EXISTS policy_assignments_policy_id_idx ON policy_assignments(policy_id);
CREATE INDEX IF NOT EXISTS policy_assignments_tenant_id_idx ON policy_assignments(tenant_id);
CREATE INDEX IF NOT EXISTS policy_assignments_node_id_idx ON policy_assignments(node_id);



