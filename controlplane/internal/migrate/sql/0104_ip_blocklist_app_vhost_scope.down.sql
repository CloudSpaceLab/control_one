DROP INDEX IF EXISTS idx_ip_blocklist_entries_app_scope;

ALTER TABLE ip_blocklist_entries
    DROP COLUMN IF EXISTS vhost,
    DROP COLUMN IF EXISTS app;
