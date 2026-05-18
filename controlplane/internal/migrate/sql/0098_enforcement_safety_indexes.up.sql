CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_tenant_created
    ON ip_blocklist_entries (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_created
    ON ip_blocklist_entries (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_webserver_config_actions_failures
    ON webserver_config_actions (tenant_id, node_id, action, status, updated_at DESC);
