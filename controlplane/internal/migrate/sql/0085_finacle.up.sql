-- Finacle integration (Use Case 6).
--
-- Three tables:
--   * finacle_connections    — one connection config per tenant per Finacle host.
--                              credential_ref points into secret_groups so we
--                              never persist OAuth secrets in plaintext.
--   * finacle_shift_configs  — branch-scoped shift definitions (3-shift, 2-shift,
--                              branch hours, always-on). The shifts JSONB encodes
--                              the time bands; grace_minutes adds slack on either
--                              side so an in-session user is not yanked at the
--                              instant of rollover.
--   * finacle_profiles       — synced Finacle UID per branch+role; bound to a
--                              shift_id. The shift_rotate worker uses
--                              ListProfilesByShift to enable/disable access
--                              when a shift boundary fires.

CREATE TABLE IF NOT EXISTS finacle_connections (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL,
    host            TEXT         NOT NULL,
    auth_method     TEXT         NOT NULL CHECK (auth_method IN ('oauth2_client_credentials','basic')),
    credential_ref  TEXT,
    last_sync_at    TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finacle_connections_tenant
    ON finacle_connections (tenant_id);

CREATE TABLE IF NOT EXISTS finacle_shift_configs (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL,
    branch_id       TEXT,
    model           TEXT         NOT NULL CHECK (model IN ('3_shift','2_shift','branch_hours','always_on')),
    shifts          JSONB        NOT NULL DEFAULT '[]'::jsonb,
    grace_minutes   INTEGER      NOT NULL DEFAULT 15,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finacle_shift_configs_tenant
    ON finacle_shift_configs (tenant_id);
CREATE INDEX IF NOT EXISTS idx_finacle_shift_configs_branch
    ON finacle_shift_configs (tenant_id, branch_id);

CREATE TABLE IF NOT EXISTS finacle_profiles (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL,
    finacle_uid     TEXT         NOT NULL,
    branch_id       TEXT,
    role            TEXT,
    shift_id        UUID         REFERENCES finacle_shift_configs(id) ON DELETE SET NULL,
    status          TEXT         NOT NULL DEFAULT 'unknown',
    last_rotated_at TIMESTAMPTZ,
    UNIQUE (tenant_id, finacle_uid)
);

CREATE INDEX IF NOT EXISTS idx_finacle_profiles_tenant
    ON finacle_profiles (tenant_id);
CREATE INDEX IF NOT EXISTS idx_finacle_profiles_branch
    ON finacle_profiles (tenant_id, branch_id);
CREATE INDEX IF NOT EXISTS idx_finacle_profiles_shift
    ON finacle_profiles (shift_id);
