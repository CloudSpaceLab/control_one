CREATE TABLE IF NOT EXISTS action_plan_approvals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_plan_id  UUID        NOT NULL REFERENCES action_plans(id) ON DELETE CASCADE,
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    decision        TEXT        NOT NULL CHECK (decision IN ('approved', 'denied')),
    actor_id        UUID        REFERENCES users(id) ON DELETE SET NULL,
    actor_subject   TEXT        NOT NULL DEFAULT '',
    actor_key       TEXT        NOT NULL,
    actor_roles     TEXT[]      NOT NULL DEFAULT '{}',
    note            TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (action_plan_id, actor_key)
);

CREATE INDEX IF NOT EXISTS idx_action_plan_approvals_plan
    ON action_plan_approvals (action_plan_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_action_plan_approvals_tenant
    ON action_plan_approvals (tenant_id, created_at DESC);

COMMENT ON TABLE action_plan_approvals IS 'Append-only operator approval/denial records for unified action plans';
COMMENT ON COLUMN action_plan_approvals.actor_key IS 'Stable actor identity used to prevent one operator from satisfying multiple required approvals for a plan';
