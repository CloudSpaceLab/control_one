CREATE TABLE IF NOT EXISTS private_access_provider_accounts (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider                TEXT NOT NULL CHECK (provider IN ('netbird', 'headscale', 'openziti')),
    account_id              TEXT NOT NULL DEFAULT 'default',
    display_name            TEXT NOT NULL DEFAULT '',
    endpoint_url            TEXT NOT NULL DEFAULT '',
    credential_id           UUID REFERENCES provider_credentials(id) ON DELETE SET NULL,
    status                  TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'error')),
    config                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    import_enabled          BOOLEAN NOT NULL DEFAULT false,
    import_interval_seconds INTEGER NOT NULL DEFAULT 3600 CHECK (import_interval_seconds >= 300),
    next_import_at          TIMESTAMPTZ,
    last_import_at          TIMESTAMPTZ,
    last_import_status      TEXT NOT NULL DEFAULT '',
    last_import_error       TEXT NOT NULL DEFAULT '',
    created_by_subject      TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, account_id)
);

CREATE INDEX IF NOT EXISTS idx_private_access_provider_accounts_tenant
    ON private_access_provider_accounts (tenant_id, provider, status);

CREATE INDEX IF NOT EXISTS idx_private_access_provider_accounts_due
    ON private_access_provider_accounts (next_import_at)
    WHERE import_enabled = true AND status = 'active';

CREATE TABLE IF NOT EXISTS private_access_import_runs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider_account_id UUID NOT NULL REFERENCES private_access_provider_accounts(id) ON DELETE CASCADE,
    job_id              UUID REFERENCES jobs(id) ON DELETE SET NULL,
    provider            TEXT NOT NULL CHECK (provider IN ('netbird', 'headscale', 'openziti')),
    account_id          TEXT NOT NULL DEFAULT 'default',
    status              TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    summary             JSONB NOT NULL DEFAULT '{}'::jsonb,
    error               TEXT NOT NULL DEFAULT '',
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_private_access_import_runs_account_created
    ON private_access_import_runs (provider_account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_private_access_import_runs_tenant_created
    ON private_access_import_runs (tenant_id, created_at DESC);
