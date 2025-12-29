DROP TABLE IF EXISTS provisioning_template_rollouts;

ALTER TABLE provisioning_templates
    DROP CONSTRAINT IF EXISTS provisioning_templates_promoted_version_id_fkey,
    DROP COLUMN IF EXISTS promoted_version_id;

DROP INDEX IF EXISTS provisioning_template_rollouts_state_idx;
DROP INDEX IF EXISTS provisioning_template_rollouts_version_idx;
DROP INDEX IF EXISTS provisioning_template_versions_promoted_at_idx;
DROP INDEX IF EXISTS provisioning_template_versions_template_id_idx;

DROP TABLE IF EXISTS provisioning_template_versions;
DROP TABLE IF EXISTS provisioning_templates;
