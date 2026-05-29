CREATE TABLE IF NOT EXISTS content_pack_source_proposals (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id               UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    proposal_id           TEXT        NOT NULL DEFAULT '',
    kind                  TEXT        NOT NULL DEFAULT '',
    program               TEXT        NOT NULL DEFAULT '',
    source_id             TEXT        NOT NULL DEFAULT '',
    collector_type        TEXT        NOT NULL DEFAULT '',
    formatter             TEXT        NOT NULL DEFAULT '',
    status                TEXT        NOT NULL DEFAULT 'proposed',
    confidence            INTEGER     NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 100),
    risk                  TEXT        NOT NULL DEFAULT '',
    auto_connect_eligible BOOLEAN     NOT NULL DEFAULT FALSE,
    requires_approval     BOOLEAN     NOT NULL DEFAULT FALSE,
    reason                TEXT        NOT NULL DEFAULT '',
    paths                 TEXT[]      NOT NULL DEFAULT ARRAY[]::TEXT[],
    evidence              TEXT[]      NOT NULL DEFAULT ARRAY[]::TEXT[],
    labels                JSONB       NOT NULL DEFAULT '{}'::JSONB,
    first_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    approved_by_subject   TEXT        NOT NULL DEFAULT '',
    approved_at           TIMESTAMPTZ,
    approval_note         TEXT        NOT NULL DEFAULT '',
    collect_mode          TEXT        NOT NULL DEFAULT '',
    rejected_by_subject   TEXT        NOT NULL DEFAULT '',
    rejected_at           TIMESTAMPTZ,
    rejection_reason      TEXT        NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, node_id, proposal_id),
    CHECK (status IN ('proposed', 'auto_eligible', 'approval_required', 'approved', 'rejected', 'privacy_blocked', 'stale')),
    CHECK (collect_mode IN ('', 'observe_only', 'metadata_only', 'collect_parsed', 'collect_raw', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_content_pack_source_proposals_tenant_status
    ON content_pack_source_proposals (tenant_id, status, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_pack_source_proposals_node
    ON content_pack_source_proposals (node_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_pack_source_proposals_program
    ON content_pack_source_proposals (tenant_id, program);
