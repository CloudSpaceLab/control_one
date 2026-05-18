ALTER TABLE ip_blocklist_entries
    ADD COLUMN IF NOT EXISTS protected_override BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS protected_override_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_protected_override
    ON ip_blocklist_entries (tenant_id, protected_override, created_at DESC)
    WHERE protected_override = TRUE;
