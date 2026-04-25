CREATE TABLE IF NOT EXISTS user_mfa_factors (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    factor_type    TEXT NOT NULL CHECK (factor_type IN ('totp','webauthn')),
    label          TEXT,
    secret_sealed  BYTEA NOT NULL,
    nonce          BYTEA NOT NULL,
    webauthn_cred_id TEXT,
    sign_count     BIGINT NOT NULL DEFAULT 0,
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at   TIMESTAMPTZ,
    UNIQUE (user_id, factor_type, label)
);

CREATE INDEX IF NOT EXISTS idx_mfa_user    ON user_mfa_factors (user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_type    ON user_mfa_factors (factor_type);
CREATE INDEX IF NOT EXISTS idx_mfa_enabled ON user_mfa_factors (enabled);

CREATE TABLE IF NOT EXISTS step_up_challenges (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action       TEXT NOT NULL,
    resource_id  TEXT,
    challenge    BYTEA NOT NULL,
    consumed     BOOLEAN NOT NULL DEFAULT false,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consumed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_step_up_user     ON step_up_challenges (user_id);
CREATE INDEX IF NOT EXISTS idx_step_up_expires  ON step_up_challenges (expires_at);
CREATE INDEX IF NOT EXISTS idx_step_up_unconsumed ON step_up_challenges (user_id, action) WHERE consumed = false;
