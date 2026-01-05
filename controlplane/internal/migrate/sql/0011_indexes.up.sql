-- Performance indexes for existing tables

-- Jobs table indexes
CREATE INDEX IF NOT EXISTS jobs_tenant_status_idx ON jobs(tenant_id, status);
CREATE INDEX IF NOT EXISTS jobs_tenant_created_idx ON jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_status_created_idx ON jobs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_type_idx ON jobs(type);

-- Audit logs indexes
CREATE INDEX IF NOT EXISTS audit_logs_actor_type_idx ON audit_logs(actor_type);
CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs(action);
CREATE INDEX IF NOT EXISTS audit_logs_created_at_idx ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_tenant_created_idx ON audit_logs(tenant_id, created_at DESC);

-- Compliance results indexes
CREATE INDEX IF NOT EXISTS compliance_results_checked_at_idx ON compliance_results(checked_at DESC);
CREATE INDEX IF NOT EXISTS compliance_results_node_checked_idx ON compliance_results(node_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS compliance_results_passed_idx ON compliance_results(passed);
CREATE INDEX IF NOT EXISTS compliance_results_severity_idx ON compliance_results(severity);

-- Nodes table indexes
CREATE INDEX IF NOT EXISTS nodes_hostname_idx ON nodes(hostname);
CREATE INDEX IF NOT EXISTS nodes_tenant_hostname_idx ON nodes(tenant_id, hostname);

-- Provisioning templates indexes
CREATE INDEX IF NOT EXISTS provisioning_templates_provider_idx ON provisioning_templates(provider);
CREATE INDEX IF NOT EXISTS provisioning_templates_name_idx ON provisioning_templates(name);



