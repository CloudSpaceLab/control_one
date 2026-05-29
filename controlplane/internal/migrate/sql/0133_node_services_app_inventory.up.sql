ALTER TABLE node_services
    ADD COLUMN IF NOT EXISTS working_dir TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS command_line TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_root TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_profile_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS app_confidence INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS app_evidence JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_node_services_app_root ON node_services(app_root) WHERE app_root <> '';
CREATE INDEX IF NOT EXISTS idx_node_services_app_profile ON node_services(app_profile_id) WHERE app_profile_id <> '';
