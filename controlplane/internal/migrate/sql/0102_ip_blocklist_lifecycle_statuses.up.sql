ALTER TABLE ip_blocklist_entries
    DROP CONSTRAINT IF EXISTS ip_blocklist_entries_status_check;

ALTER TABLE ip_blocklist_entries
    ADD CONSTRAINT ip_blocklist_entries_status_check
    CHECK (status IN ('proposed','approved','canary','dispatching','active','failed','expired','removed','denied','rejected','rolled_back'));
