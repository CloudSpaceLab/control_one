ALTER TABLE ip_blocklist_entries
    ADD COLUMN IF NOT EXISTS entity_action_id UUID REFERENCES entity_actions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_entity_action
    ON ip_blocklist_entries (entity_action_id);

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_expires
    ON ip_blocklist_entries (expires_at)
    WHERE expires_at IS NOT NULL AND status IN ('proposed','approved','dispatching','active');
