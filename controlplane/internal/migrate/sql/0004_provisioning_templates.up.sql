CREATE TABLE IF NOT EXISTS provisioning_templates (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    provider TEXT,
    description TEXT,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS provisioning_template_versions (
    id UUID PRIMARY KEY,
    template_id UUID NOT NULL REFERENCES provisioning_templates(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    checksum TEXT,
    body TEXT NOT NULL,
    metadata_schema JSONB,
    rollout_notes TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    promoted_at TIMESTAMPTZ,
    UNIQUE (template_id, version)
);

CREATE INDEX IF NOT EXISTS provisioning_template_versions_template_id_idx
    ON provisioning_template_versions(template_id);
CREATE INDEX IF NOT EXISTS provisioning_template_versions_promoted_at_idx
    ON provisioning_template_versions(promoted_at);

ALTER TABLE provisioning_templates
    ADD COLUMN IF NOT EXISTS promoted_version_id UUID;

DO $$ BEGIN
    ALTER TABLE provisioning_templates
        ADD CONSTRAINT provisioning_templates_promoted_version_id_fkey
            FOREIGN KEY (promoted_version_id)
            REFERENCES provisioning_template_versions(id)
            ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS provisioning_template_rollouts (
    id UUID PRIMARY KEY,
    template_version_id UUID NOT NULL REFERENCES provisioning_template_versions(id) ON DELETE CASCADE,
    target_percent INTEGER NOT NULL CHECK (target_percent >= 0 AND target_percent <= 100),
    state TEXT NOT NULL DEFAULT 'scheduled',
    metadata JSONB,
    scheduled_for TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS provisioning_template_rollouts_version_idx
    ON provisioning_template_rollouts(template_version_id);
CREATE INDEX IF NOT EXISTS provisioning_template_rollouts_state_idx
    ON provisioning_template_rollouts(state);
