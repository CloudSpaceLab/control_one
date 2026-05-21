DROP INDEX IF EXISTS idx_node_services_app_profile;
DROP INDEX IF EXISTS idx_node_services_app_root;

ALTER TABLE node_services
    DROP COLUMN IF EXISTS app_evidence,
    DROP COLUMN IF EXISTS app_confidence,
    DROP COLUMN IF EXISTS app_name,
    DROP COLUMN IF EXISTS app_profile_id,
    DROP COLUMN IF EXISTS app_root,
    DROP COLUMN IF EXISTS command_line,
    DROP COLUMN IF EXISTS working_dir;
