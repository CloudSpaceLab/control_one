ALTER TABLE content_pack_collector_config_candidates
    DROP COLUMN IF EXISTS reviewed_yaml_sha256,
    DROP COLUMN IF EXISTS reviewed_config_version;
