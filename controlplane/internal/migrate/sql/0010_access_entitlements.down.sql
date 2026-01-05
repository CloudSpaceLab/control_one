DROP INDEX IF EXISTS access_syncs_status_idx;
DROP INDEX IF EXISTS access_syncs_synced_at_idx;
DROP INDEX IF EXISTS access_syncs_node_id_idx;
DROP INDEX IF EXISTS access_syncs_tenant_id_idx;

DROP INDEX IF EXISTS access_entitlements_active_idx;
DROP INDEX IF EXISTS access_entitlements_revoked_at_idx;
DROP INDEX IF EXISTS access_entitlements_expires_at_idx;
DROP INDEX IF EXISTS access_entitlements_user_id_idx;
DROP INDEX IF EXISTS access_entitlements_node_id_idx;
DROP INDEX IF EXISTS access_entitlements_tenant_id_idx;

DROP TABLE IF EXISTS access_syncs;
DROP TABLE IF EXISTS access_entitlements;



