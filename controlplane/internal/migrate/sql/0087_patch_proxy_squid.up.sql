-- Wave C — Patch Management completion (PR #30 follow-up).
-- Adds three tables on top of 0083 (patch_deployments + node_patch_state):
--   node_patch_config   — per-node mode (direct | proxy | airgapped)
--   maintenance_windows — operator-scheduled change windows + allow-repo set
--   squid_proxies       — managed Squid CONNECT-style HTTP proxies
--
-- All three are tenant-scoped where applicable. node_patch_config rides on
-- nodes(id) ON DELETE CASCADE so a retired node's config disappears.

CREATE TABLE IF NOT EXISTS node_patch_config (
    node_id     UUID         PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    mode        TEXT         NOT NULL DEFAULT 'direct'
                              CHECK (mode IN ('direct', 'proxy', 'airgapped')),
    proxy_id    UUID,
    window_id   UUID,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS maintenance_windows (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL,
    name            TEXT         NOT NULL,
    node_ids        UUID[]       NOT NULL DEFAULT '{}',
    opens_at        TIMESTAMPTZ  NOT NULL,
    closes_at       TIMESTAMPTZ  NOT NULL,
    allow_repos     TEXT[]       NOT NULL DEFAULT '{}',
    status          TEXT         NOT NULL DEFAULT 'scheduled'
                                  CHECK (status IN ('scheduled', 'open', 'closing', 'closed', 'aborted')),
    opened_by       UUID,
    force_closed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_maintenance_windows_tenant_status
    ON maintenance_windows (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_maintenance_windows_opens_at
    ON maintenance_windows (opens_at);

CREATE TABLE IF NOT EXISTS squid_proxies (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID         NOT NULL,
    host              TEXT         NOT NULL,
    port              INTEGER      NOT NULL DEFAULT 3128,
    status            TEXT         NOT NULL DEFAULT 'installing'
                                    CHECK (status IN ('installing', 'healthy', 'degraded', 'removing', 'removed')),
    whitelist         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    last_validated_at TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, host, port)
);

CREATE INDEX IF NOT EXISTS idx_squid_proxies_tenant
    ON squid_proxies (tenant_id, status);
