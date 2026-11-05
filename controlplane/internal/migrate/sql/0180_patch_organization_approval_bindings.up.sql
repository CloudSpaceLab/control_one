ALTER TABLE patch_organization_policy ADD COLUMN bound_controls jsonb NOT NULL DEFAULT '{}';
ALTER TABLE patch_organization_history ADD COLUMN bound_controls jsonb NOT NULL DEFAULT '{}';
CREATE TABLE patch_organization_approval_bindings (
 approval_id uuid PRIMARY KEY REFERENCES patch_approvals(id),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 organization_revision bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 FOREIGN KEY(tenant_id,organization_revision) REFERENCES patch_organization_history(tenant_id,revision)
);
