ALTER TABLE ip_blocklist_entries
    ADD COLUMN IF NOT EXISTS server_group TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_server_group_created
    ON ip_blocklist_entries (tenant_id, server_group, created_at DESC)
    WHERE server_group <> '';
