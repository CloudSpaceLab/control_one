-- Reverse 0022_nodes_machine_id.up.sql
DROP INDEX IF EXISTS idx_nodes_state;
ALTER TABLE nodes DROP COLUMN IF EXISTS state;

DROP INDEX IF EXISTS idx_nodes_tenant_machine_id;
ALTER TABLE nodes DROP COLUMN IF EXISTS machine_id;
