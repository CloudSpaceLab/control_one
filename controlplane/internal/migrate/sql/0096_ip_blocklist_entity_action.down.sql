DROP INDEX IF EXISTS idx_ip_blocklist_entries_expires;
DROP INDEX IF EXISTS idx_ip_blocklist_entries_entity_action;

ALTER TABLE ip_blocklist_entries
    DROP COLUMN IF EXISTS entity_action_id;
