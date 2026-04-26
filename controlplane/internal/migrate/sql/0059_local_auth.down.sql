DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS user_sessions;
DROP INDEX IF EXISTS users_email_unique_idx;
ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS auth_provider,
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS password_changed_at;
