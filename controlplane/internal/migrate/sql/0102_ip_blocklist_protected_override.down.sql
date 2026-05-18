DROP INDEX IF EXISTS idx_ip_blocklist_entries_protected_override;

ALTER TABLE ip_blocklist_entries
    DROP COLUMN IF EXISTS protected_override_reason,
    DROP COLUMN IF EXISTS protected_override;
