CREATE TABLE IF NOT EXISTS action_plans (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id              UUID        REFERENCES nodes(id) ON DELETE SET NULL,
    domain               TEXT        NOT NULL,
    action_kind          TEXT        NOT NULL,
    state                TEXT        NOT NULL DEFAULT 'proposed'
                                      CHECK (state IN (
                                          'draft',
                                          'proposed',
                                          'needs_approval',
                                          'approved',
                                          'queued',
                                          'running',
                                          'succeeded',
                                          'failed',
                                          'verified',
                                          'rolled_back',
                                          'cancelled'
                                      )),
    risk                 TEXT        NOT NULL DEFAULT 'medium',
    scope                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    diff                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    required_approvals   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    maintenance_window   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rollback_plan        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    verification_plan    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key      TEXT,
    created_by           UUID        REFERENCES users(id) ON DELETE SET NULL,
    source_ref           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_action_plans_tenant_state
    ON action_plans (tenant_id, state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_action_plans_node
    ON action_plans (node_id, created_at DESC)
    WHERE node_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_action_plans_domain
    ON action_plans (tenant_id, domain, action_kind, created_at DESC);

COMMENT ON TABLE action_plans IS 'Unified durable plan contract for remediation, patch, firewall, webserver, private-access, and AI-driven actions';

CREATE TABLE IF NOT EXISTS action_receipts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_plan_id  UUID        NOT NULL REFERENCES action_plans(id) ON DELETE CASCADE,
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id         UUID        REFERENCES nodes(id) ON DELETE SET NULL,
    job_id          UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    state           TEXT        NOT NULL CHECK (state IN (
                                'queued',
                                'running',
                                'succeeded',
                                'failed',
                                'verified',
                                'rolled_back',
                                'cancelled'
                            )),
    receipt         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    verification    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rollback_ref    TEXT        NOT NULL DEFAULT '',
    error           TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_action_receipts_plan
    ON action_receipts (action_plan_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_action_receipts_job
    ON action_receipts (job_id)
    WHERE job_id IS NOT NULL;

COMMENT ON TABLE action_receipts IS 'Append-only execution and verification receipts for unified action plans';
