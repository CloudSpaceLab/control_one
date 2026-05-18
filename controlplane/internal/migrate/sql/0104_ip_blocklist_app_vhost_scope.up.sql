ALTER TABLE ip_blocklist_entries
    ADD COLUMN IF NOT EXISTS app TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS vhost TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_app_scope
    ON ip_blocklist_entries (tenant_id, app, vhost, status, created_at DESC)
    WHERE app <> '' OR vhost <> '';
