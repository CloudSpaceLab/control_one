-- Production upgrade backfill for environments where migration number 0112 was
-- already consumed before AI LogFixer tables were added to the tree.

CREATE TABLE IF NOT EXISTS ai_logfixer_runs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id                 UUID        REFERENCES nodes(id) ON DELETE SET NULL,
    investigation_id        UUID        REFERENCES ai_investigations(id) ON DELETE SET NULL,
    proposal_id             UUID        REFERENCES ai_operator_proposals(id) ON DELETE SET NULL,
    job_id                  UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    approval_id             UUID,
    trigger_event_type      TEXT        NOT NULL,
    trigger_dedup_key       TEXT        NOT NULL,
    service_key             TEXT        NOT NULL DEFAULT '',
    status                  TEXT        NOT NULL DEFAULT 'planned'
                                          CHECK (status IN (
                                              'pending',
                                              'planned',
                                              'planning',
                                              'awaiting_approval',
                                              'approved',
                                              'denied',
                                              'running',
                                              'monitoring',
                                              'succeeded',
                                              'failed',
                                              'rolled_back',
                                              'escalated'
                                          )),
    risk_level              TEXT        NOT NULL DEFAULT 'read_only'
                                          CHECK (risk_level IN (
                                              'read_only',
                                              'low_risk',
                                              'medium_risk',
                                              'high_risk',
                                              'critical_risk',
                                              'blocked'
                                          )),
    investigation_request   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    diagnosis               JSONB       NOT NULL DEFAULT '{}'::jsonb,
    remediation_plan        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    attempt                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    receipt                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    evidence                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, trigger_dedup_key)
);

CREATE INDEX IF NOT EXISTS idx_ai_logfixer_runs_tenant_status
    ON ai_logfixer_runs (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_logfixer_runs_node
    ON ai_logfixer_runs (node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_logfixer_runs_investigation
    ON ai_logfixer_runs (investigation_id);
CREATE INDEX IF NOT EXISTS idx_ai_logfixer_runs_job
    ON ai_logfixer_runs (job_id);

COMMENT ON TABLE ai_logfixer_runs IS 'Control One-owned run records for AI LogFixer contract artifacts, approvals, jobs, and receipts';

CREATE TABLE IF NOT EXISTS ai_logfixer_actions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id         UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    run_id          UUID        REFERENCES ai_logfixer_runs(id) ON DELETE SET NULL,
    job_id          UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    action          TEXT        NOT NULL CHECK (action IN ('ailogfixer.plan', 'ailogfixer.apply', 'ailogfixer.rollback')),
    status          TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    policy          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    result          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id)
);

CREATE INDEX IF NOT EXISTS idx_ai_logfixer_actions_node_pending
    ON ai_logfixer_actions (node_id, status, updated_at)
    WHERE status IN ('pending', 'running');
CREATE INDEX IF NOT EXISTS idx_ai_logfixer_actions_run
    ON ai_logfixer_actions (run_id, created_at DESC);

COMMENT ON TABLE ai_logfixer_actions IS 'Heartbeat-dispatched AI LogFixer node actions and returned execution receipts';
