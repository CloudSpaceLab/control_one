DROP INDEX IF EXISTS idx_ip_blocklist_entries_server_group_created;

ALTER TABLE ip_blocklist_entries
    DROP COLUMN IF EXISTS server_group;
