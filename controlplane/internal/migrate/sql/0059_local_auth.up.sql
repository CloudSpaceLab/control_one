-- Local email/password auth + LDAP + session tokens.
--
-- Adds password_hash + auth_provider + last_login_at to users so the same
-- table backs every auth path:
--   * `local`    — bcrypt hash in password_hash; admin-provisioned only
--   * `ldap`     — password_hash NULL; auth via LDAP bind on each login
--   * `oidc`     — pre-existing path; password_hash NULL
--
-- user_sessions stores the opaque bearer token issued post-login so the
-- middleware can validate without re-running bcrypt or LDAP every request.
-- token_hash is sha256(token) so a DB leak doesn't reveal live sessions.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS password_hash      TEXT,
    ADD COLUMN IF NOT EXISTS auth_provider      TEXT NOT NULL DEFAULT 'oidc',
    ADD COLUMN IF NOT EXISTS last_login_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS disabled_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ;

-- Email is already a column; ensure unique-when-non-null + indexed for
-- login lookup. Partial unique index treats NULL emails (legacy oidc-only
-- users) as distinct.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_idx
    ON users (LOWER(email)) WHERE email IS NOT NULL;

CREATE TABLE IF NOT EXISTS user_sessions (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT NOT NULL UNIQUE,
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    last_used_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,
    user_agent      TEXT,
    ip_address      INET
);
CREATE INDEX IF NOT EXISTS user_sessions_user_idx ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS user_sessions_expires_idx ON user_sessions (expires_at);

-- Permissions registry: each permission is a string key like
-- "tenants.read", "rules.write", "remediation.approve". Roles get a set of
-- permissions via role_permissions. CISO admins manage these via /api/v1/roles.
CREATE TABLE IF NOT EXISTS permissions (
    name        TEXT PRIMARY KEY,
    description TEXT,
    category    TEXT NOT NULL DEFAULT 'general',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id        UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_name TEXT NOT NULL REFERENCES permissions(name) ON DELETE CASCADE,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_name)
);

-- Seed the canonical permission catalog. Adding a new permission later is
-- a regular INSERT.
INSERT INTO permissions (name, description, category) VALUES
    ('tenants.read',       'List + view tenants',                  'tenants'),
    ('tenants.write',      'Create, update, delete tenants',       'tenants'),
    ('nodes.read',         'List + view nodes',                    'nodes'),
    ('nodes.write',        'Enroll, update, retire nodes',         'nodes'),
    ('rules.read',         'View detection + remediation rules',   'rules'),
    ('rules.write',        'Create + edit rules',                  'rules'),
    ('alerts.read',        'View alerts',                          'alerts'),
    ('alerts.acknowledge', 'Acknowledge / silence alerts',         'alerts'),
    ('compliance.read',    'View compliance results',              'compliance'),
    ('compliance.run',     'Trigger compliance scans',             'compliance'),
    ('remediation.read',   'View remediation jobs',                'remediation'),
    ('remediation.approve','Approve remediation jobs',             'remediation'),
    ('audit.read',         'View audit log',                       'audit'),
    ('users.read',         'List + view operators',                'users'),
    ('users.write',        'Create, update, disable operators',    'users'),
    ('roles.read',         'View roles + permissions',             'rbac'),
    ('roles.write',        'Edit roles + permissions',             'rbac'),
    ('threat_feeds.read',  'View threat feeds',                    'threats'),
    ('threat_feeds.write', 'Create + edit threat feeds',           'threats'),
    ('connections.read',   'View connection forensics',            'forensics'),
    ('files.read',         'View file-access events',              'forensics'),
    ('dbquery.read',       'View DB query events',                 'forensics'),
    ('events.ingest',      'Agent event ingest',                   'agent'),
    ('dashboards.read',    'View custom dashboards',               'dashboards'),
    ('dashboards.write',   'Create + edit custom dashboards',      'dashboards'),
    ('settings.read',      'View tenant settings',                 'settings'),
    ('settings.write',     'Edit tenant settings',                 'settings'),
    ('bastion.use',        'Open bastion sessions',                'bastion')
ON CONFLICT (name) DO NOTHING;

-- Ensure the four built-in roles exist. Existing rows with the same name
-- but different UUIDs (legacy bootstraps) keep their UUIDs — we always
-- look them up by name when granting permissions, so role_id values
-- don't have to be stable.
INSERT INTO roles (id, name, description) VALUES
    (gen_random_uuid(), 'admin',    'Full system access — everything'),
    (gen_random_uuid(), 'ciso',     'CISO admin — RBAC, policies, full forensics, no infra mutation'),
    (gen_random_uuid(), 'operator', 'SecOps operator — alerts, remediation, rules'),
    (gen_random_uuid(), 'viewer',   'Read-only — dashboards, alerts, forensics view')
ON CONFLICT (name) DO NOTHING;

-- admin: every permission.
INSERT INTO role_permissions (role_id, permission_name)
SELECT (SELECT id FROM roles WHERE name='admin'), name FROM permissions
ON CONFLICT DO NOTHING;

-- ciso: read everything + manage RBAC + remediation approvals.
INSERT INTO role_permissions (role_id, permission_name)
SELECT (SELECT id FROM roles WHERE name='ciso'), unnest(ARRAY[
    'tenants.read','nodes.read','rules.read','rules.write',
    'alerts.read','alerts.acknowledge','compliance.read',
    'remediation.read','remediation.approve','audit.read',
    'users.read','users.write','roles.read','roles.write',
    'threat_feeds.read','threat_feeds.write',
    'connections.read','files.read','dbquery.read',
    'dashboards.read','dashboards.write',
    'settings.read','settings.write'])
ON CONFLICT DO NOTHING;

-- operator: alerts + remediation + rules write, no RBAC, no settings.
INSERT INTO role_permissions (role_id, permission_name)
SELECT (SELECT id FROM roles WHERE name='operator'), unnest(ARRAY[
    'tenants.read','nodes.read','nodes.write','rules.read','rules.write',
    'alerts.read','alerts.acknowledge','compliance.read','compliance.run',
    'remediation.read','audit.read','threat_feeds.read',
    'connections.read','files.read','dbquery.read',
    'dashboards.read','dashboards.write','bastion.use'])
ON CONFLICT DO NOTHING;

-- viewer: read-only across the operations surface.
INSERT INTO role_permissions (role_id, permission_name)
SELECT (SELECT id FROM roles WHERE name='viewer'), unnest(ARRAY[
    'tenants.read','nodes.read','rules.read','alerts.read',
    'compliance.read','remediation.read','threat_feeds.read',
    'connections.read','files.read','dbquery.read','dashboards.read'])
ON CONFLICT DO NOTHING;
