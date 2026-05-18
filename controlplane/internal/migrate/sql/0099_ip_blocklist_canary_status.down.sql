DROP INDEX IF EXISTS idx_webserver_config_actions_block_entry_metadata;
DROP INDEX IF EXISTS idx_webserver_config_actions_block_entry;

ALTER TABLE ip_blocklist_entries
    DROP CONSTRAINT IF EXISTS ip_blocklist_entries_status_check;

ALTER TABLE ip_blocklist_entries
    ADD CONSTRAINT ip_blocklist_entries_status_check
    CHECK (status IN ('proposed','approved','dispatching','active','failed','expired','removed','denied'));
