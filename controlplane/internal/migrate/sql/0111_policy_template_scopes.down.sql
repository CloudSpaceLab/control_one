DROP INDEX IF EXISTS provisioning_template_assignments_selector_gin_idx;
DROP INDEX IF EXISTS provisioning_template_assignments_scope_lookup_idx;
DROP INDEX IF EXISTS provisioning_template_assignments_template_idx;
DROP INDEX IF EXISTS provisioning_template_assignments_scope_unique_idx;
DROP TABLE IF EXISTS provisioning_template_assignments;

DROP INDEX IF EXISTS provisioning_templates_tenant_id_idx;
DROP INDEX IF EXISTS provisioning_templates_tenant_name_unique_idx;
ALTER TABLE provisioning_templates
    DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE provisioning_templates
    ADD CONSTRAINT provisioning_templates_name_key UNIQUE (name);

DROP INDEX IF EXISTS policy_assignments_selector_gin_idx;
DROP INDEX IF EXISTS policy_assignments_scope_lookup_idx;
DROP INDEX IF EXISTS policy_assignments_scope_unique_idx;

ALTER TABLE policy_assignments
    DROP CONSTRAINT IF EXISTS policy_assignments_scope_check,
    DROP COLUMN IF EXISTS selector,
    DROP COLUMN IF EXISTS scope_id,
    DROP COLUMN IF EXISTS scope_type;

ALTER TABLE policy_assignments
    ADD CONSTRAINT policy_assignments_policy_id_tenant_id_node_id_key
    UNIQUE (policy_id, tenant_id, node_id);
