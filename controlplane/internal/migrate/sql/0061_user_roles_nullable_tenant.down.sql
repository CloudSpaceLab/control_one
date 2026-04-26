DROP INDEX IF EXISTS user_roles_global_uniq;
DROP INDEX IF EXISTS user_roles_tenant_uniq;
ALTER TABLE user_roles DROP COLUMN IF EXISTS id;
-- Old PK cannot contain NULL rows; remove any global assignments first.
DELETE FROM user_roles WHERE tenant_id IS NULL;
ALTER TABLE user_roles ADD PRIMARY KEY (user_id, role_id, tenant_id);
