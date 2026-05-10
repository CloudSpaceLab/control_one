ALTER TABLE tenant_remediation_config
    DROP COLUMN IF EXISTS patch_requires_approval;

DROP INDEX IF EXISTS idx_patch_approvals_expires_at;
DROP INDEX IF EXISTS idx_patch_approvals_deployment;
DROP INDEX IF EXISTS idx_patch_approvals_tenant_status;
DROP TABLE IF EXISTS patch_approvals;
