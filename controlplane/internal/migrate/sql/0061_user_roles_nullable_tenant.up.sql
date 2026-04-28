-- user_roles had a composite PRIMARY KEY (user_id, role_id, tenant_id).
-- PostgreSQL implicitly adds NOT NULL to all PK columns, which prevents
-- inserting system-global (tenant_id = NULL) role assignments.
--
-- Fix: replace the composite PK with a surrogate UUID PK and two partial
-- unique indexes that provide equivalent deduplication while allowing NULL.

ALTER TABLE user_roles DROP CONSTRAINT user_roles_pkey;

-- Dropping the PK removes the PK-enforced NOT NULL, but PostgreSQL keeps any
-- column-level NOT NULL constraint separately. Drop it explicitly.
ALTER TABLE user_roles ALTER COLUMN tenant_id DROP NOT NULL;

ALTER TABLE user_roles ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE user_roles ADD PRIMARY KEY (id);

-- NULL tenant_id = system-global role (e.g. admin, ciso for operator accounts)
CREATE UNIQUE INDEX user_roles_global_uniq
    ON user_roles (user_id, role_id)
    WHERE tenant_id IS NULL;

-- Non-NULL tenant_id = tenant-scoped role assignment
CREATE UNIQUE INDEX user_roles_tenant_uniq
    ON user_roles (user_id, role_id, tenant_id)
    WHERE tenant_id IS NOT NULL;
