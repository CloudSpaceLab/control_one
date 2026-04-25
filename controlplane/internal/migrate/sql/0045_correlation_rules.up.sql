CREATE TABLE IF NOT EXISTS correlation_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT,
    event_types     TEXT[] NOT NULL DEFAULT '{}',
    window_seconds  INTEGER NOT NULL DEFAULT 300 CHECK (window_seconds > 0),
    threshold       INTEGER NOT NULL DEFAULT 1 CHECK (threshold > 0),
    dimension       TEXT NOT NULL DEFAULT 'node_id',
    severity        TEXT NOT NULL DEFAULT 'high',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    yaml_spec       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_correlation_tenant  ON correlation_rules (tenant_id);
CREATE INDEX IF NOT EXISTS idx_correlation_enabled ON correlation_rules (enabled);
