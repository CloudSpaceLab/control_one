CREATE TABLE IF NOT EXISTS clusters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    desired_size INTEGER NOT NULL CHECK (desired_size >= 0),
    role_plan JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    failure_domain_strategy TEXT NOT NULL DEFAULT 'spread',
    state TEXT NOT NULL DEFAULT 'pending',
    template_id UUID REFERENCES provisioning_templates(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_clusters_tenant ON clusters (tenant_id);
CREATE INDEX IF NOT EXISTS idx_clusters_state ON clusters (state);
