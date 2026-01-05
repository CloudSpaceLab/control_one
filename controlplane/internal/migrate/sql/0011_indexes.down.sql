DROP INDEX IF EXISTS provisioning_templates_name_idx;
DROP INDEX IF EXISTS provisioning_templates_provider_idx;

DROP INDEX IF EXISTS nodes_tenant_hostname_idx;
DROP INDEX IF EXISTS nodes_hostname_idx;

DROP INDEX IF EXISTS compliance_results_severity_idx;
DROP INDEX IF EXISTS compliance_results_passed_idx;
DROP INDEX IF EXISTS compliance_results_node_checked_idx;
DROP INDEX IF EXISTS compliance_results_checked_at_idx;

DROP INDEX IF EXISTS audit_logs_tenant_created_idx;
DROP INDEX IF EXISTS audit_logs_created_at_idx;
DROP INDEX IF EXISTS audit_logs_action_idx;
DROP INDEX IF EXISTS audit_logs_actor_type_idx;

DROP INDEX IF EXISTS jobs_type_idx;
DROP INDEX IF EXISTS jobs_status_created_idx;
DROP INDEX IF EXISTS jobs_tenant_created_idx;
DROP INDEX IF EXISTS jobs_tenant_status_idx;



