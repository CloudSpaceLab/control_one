-- ai_config stores per-tenant LLM provider config for the Ask AI surface.
-- One row per tenant. api_key is stored as plain text in this initial cut —
-- a future migration moves it into the secrets infra. Feature is feature-
-- The Ask AI route is always visible; answers require tenant provider config.

CREATE TABLE IF NOT EXISTS ai_config (
    tenant_id     UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    provider      TEXT        NOT NULL DEFAULT 'anthropic',
    model         TEXT        NOT NULL DEFAULT 'claude-sonnet-4-6',
    base_url      TEXT        NOT NULL DEFAULT '',
    api_key       TEXT        NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
