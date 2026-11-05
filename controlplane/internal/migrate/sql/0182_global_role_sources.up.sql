-- Existing global grants cannot be attributed safely from assigned_by alone.
-- Preserve them as unknown; they require explicit review before reconciliation.
CREATE TABLE global_role_sources (
    assignment_id uuid NOT NULL REFERENCES user_roles(id) ON DELETE CASCADE,
    source text NOT NULL CHECK (source IN ('administrator', 'directory', 'legacy_unknown')),
    PRIMARY KEY (assignment_id, source)
);

INSERT INTO global_role_sources (assignment_id, source)
SELECT id, 'legacy_unknown' FROM user_roles WHERE tenant_id IS NULL;
