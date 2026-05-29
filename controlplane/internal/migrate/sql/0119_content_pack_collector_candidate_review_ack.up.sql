ALTER TABLE content_pack_collector_config_candidates
    ADD COLUMN IF NOT EXISTS reviewed_config_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_yaml_sha256 TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN content_pack_collector_config_candidates.reviewed_config_version IS 'Exact sha256: config_version acknowledged by the approver before candidate approval';
COMMENT ON COLUMN content_pack_collector_config_candidates.reviewed_yaml_sha256 IS 'Hex SHA-256 digest of the rendered YAML acknowledged by the approver';
