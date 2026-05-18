-- AI operator persistence (Sprint 5 closeout).
--
-- The operator-mode tools remain proposal-only. These tables make anomaly
-- investigations and dry-run action proposals durable so operators can review
-- and route them through the existing approval surfaces before any mutation.

CREATE TABLE IF NOT EXISTS ai_investigations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL,
    node_id             UUID,
    trigger_type        TEXT        NOT NULL,
    trigger_event_type  TEXT        NOT NULL,
    trigger_dedup_key   TEXT        NOT NULL,
    severity            TEXT        NOT NULL DEFAULT 'info',
    summary             TEXT        NOT NULL,
    evidence            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT        NOT NULL DEFAULT 'open'
                                    CHECK (status IN ('open', 'reviewing', 'closed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, trigger_dedup_key)
);

CREATE INDEX IF NOT EXISTS idx_ai_investigations_tenant_status
    ON ai_investigations (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_investigations_node
    ON ai_investigations (node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_investigations_trigger
    ON ai_investigations (tenant_id, trigger_type, trigger_event_type, created_at DESC);

COMMENT ON TABLE ai_investigations IS 'Durable AI/operator investigation records opened by anomaly events';
COMMENT ON COLUMN ai_investigations.evidence IS 'Original anomaly/event payload used to ground the investigation';

CREATE TABLE IF NOT EXISTS ai_operator_proposals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,
    node_id         UUID,
    action          TEXT        NOT NULL,
    reason          TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'proposed'
                                CHECK (status IN ('proposed', 'approved', 'denied', 'expired')),
    dry_run         BOOLEAN     NOT NULL DEFAULT true,
    approval_kind   TEXT        NOT NULL DEFAULT 'manual',
    approval_path   TEXT        NOT NULL DEFAULT '',
    source_tool     TEXT        NOT NULL DEFAULT 'operator_propose_action',
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_operator_proposals_tenant_status
    ON ai_operator_proposals (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_operator_proposals_node
    ON ai_operator_proposals (node_id, created_at DESC);

COMMENT ON TABLE ai_operator_proposals IS 'Dry-run operator action proposals produced by AI tools; no execution is queued here';
COMMENT ON COLUMN ai_operator_proposals.approval_path IS 'Existing approval surface operators should use before any corresponding mutation';
