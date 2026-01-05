DROP INDEX IF EXISTS policy_assignments_node_id_idx;
DROP INDEX IF EXISTS policy_assignments_tenant_id_idx;
DROP INDEX IF EXISTS policy_assignments_policy_id_idx;

DROP INDEX IF EXISTS policy_versions_promoted_at_idx;
DROP INDEX IF EXISTS policy_versions_policy_id_idx;

DROP INDEX IF EXISTS policies_archived_at_idx;
DROP INDEX IF EXISTS policies_enabled_idx;
DROP INDEX IF EXISTS policies_name_idx;
DROP INDEX IF EXISTS policies_tenant_id_idx;

DROP TABLE IF EXISTS policy_assignments;
DROP TABLE IF EXISTS policy_versions;
DROP TABLE IF EXISTS policies;



