ALTER TABLE ip_blocklist_entries
    DROP CONSTRAINT IF EXISTS ip_blocklist_entries_status_check;

ALTER TABLE ip_blocklist_entries
    ADD CONSTRAINT ip_blocklist_entries_status_check
    CHECK (status IN ('proposed','approved','canary','dispatching','active','failed','expired','removed','denied','rejected','rolled_back'));

CREATE INDEX IF NOT EXISTS idx_webserver_config_actions_block_entry
    ON webserver_config_actions ((policy->>'source_block_entry'))
    WHERE action = 'webserver.blocklist_update';

CREATE INDEX IF NOT EXISTS idx_webserver_config_actions_block_entry_metadata
    ON webserver_config_actions ((policy->'metadata'->>'ip_blocklist_entry_id'))
    WHERE action = 'webserver.blocklist_update';
