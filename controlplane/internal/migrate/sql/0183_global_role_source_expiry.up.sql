ALTER TABLE global_role_sources ADD COLUMN expires_at timestamptz;
-- Preserve existing lifetimes; do not turn an expiring grant permanent.
UPDATE global_role_sources g SET expires_at=ur.expires_at
FROM user_roles ur WHERE ur.id=g.assignment_id;
