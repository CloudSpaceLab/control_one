CREATE TABLE smtp_settings (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    tls_mode TEXT NOT NULL CHECK (tls_mode IN ('starttls', 'tls', 'none')),
    auth_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    username TEXT NOT NULL DEFAULT '',
    password_ciphertext BYTEA,
    password_nonce BYTEA,
    sender_name TEXT NOT NULL DEFAULT '',
    sender_email TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((password_ciphertext IS NULL) = (password_nonce IS NULL)),
    CHECK (NOT auth_enabled OR tls_mode <> 'none')
);
