-- Migration 0061 forgot to DROP NOT NULL on tenant_id after dropping the PK.
-- PostgreSQL keeps column-level NOT NULL separately from PK enforcement.
ALTER TABLE user_roles ALTER COLUMN tenant_id DROP NOT NULL;
