CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    max_nodes INTEGER NOT NULL DEFAULT 0,
    nodes_enrolled INTEGER NOT NULL DEFAULT 0,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_enrollment_tokens_tenant ON enrollment_tokens(tenant_id);
CREATE INDEX idx_enrollment_tokens_hash ON enrollment_tokens(token_hash);
