CREATE TABLE IF NOT EXISTS command_acl (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    role                 TEXT NOT NULL,
    node_label_selector  JSONB NOT NULL DEFAULT '{}'::jsonb,
    allow_commands       TEXT[] NOT NULL DEFAULT '{}',
    deny_commands        TEXT[] NOT NULL DEFAULT '{}',
    enabled              BOOLEAN NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_command_acl_tenant  ON command_acl (tenant_id);
CREATE INDEX IF NOT EXISTS idx_command_acl_role    ON command_acl (role);
CREATE INDEX IF NOT EXISTS idx_command_acl_enabled ON command_acl (enabled);
