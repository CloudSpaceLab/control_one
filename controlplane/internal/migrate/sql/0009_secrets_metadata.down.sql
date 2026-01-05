DROP INDEX IF EXISTS secret_syncs_status_idx;
DROP INDEX IF EXISTS secret_syncs_synced_at_idx;
DROP INDEX IF EXISTS secret_syncs_node_id_idx;
DROP INDEX IF EXISTS secret_syncs_secret_group_id_idx;

DROP INDEX IF EXISTS secret_groups_name_idx;
DROP INDEX IF EXISTS secret_groups_tenant_id_idx;

DROP TABLE IF EXISTS secret_syncs;
DROP TABLE IF EXISTS secret_groups;



