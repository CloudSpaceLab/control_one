-- Sprint 2 (Worktree C): safety rails for auto-remediation.
-- Adds per-tenant config, operator approvals queue, and circuit-breaker state.

CREATE TABLE IF NOT EXISTS tenant_remediation_config (
    tenant_id                    UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    min_approval_severity        TEXT        NOT NULL DEFAULT 'high',
    change_windows               JSONB       NOT NULL DEFAULT '[]'::jsonb,
    critical_override            BOOLEAN     NOT NULL DEFAULT true,
    circuit_breaker_window_min   INTEGER     NOT NULL DEFAULT 15  CHECK (circuit_breaker_window_min  > 0),
    circuit_breaker_fail_pct     INTEGER     NOT NULL DEFAULT 30  CHECK (circuit_breaker_fail_pct    BETWEEN 0 AND 100),
    circuit_breaker_min_samples  INTEGER     NOT NULL DEFAULT 5   CHECK (circuit_breaker_min_samples > 0),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE  tenant_remediation_config IS 'Per-tenant safety config for auto-remediation gates';
COMMENT ON COLUMN tenant_remediation_config.min_approval_severity IS 'Minimum severity (low|medium|high|critical) that requires operator approval';
COMMENT ON COLUMN tenant_remediation_config.change_windows IS 'Array of cron-like change-window specs; empty array = always open';
COMMENT ON COLUMN tenant_remediation_config.critical_override IS 'When true, critical-severity results skip change-window gating';

CREATE TABLE IF NOT EXISTS remediation_approvals (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id      UUID        NOT NULL REFERENCES nodes(id)   ON DELETE CASCADE,
    rule_id      TEXT        NOT NULL,
    script_id    UUID        NOT NULL REFERENCES remediation_scripts(id),
    severity     TEXT        NOT NULL,
    task_payload JSONB       NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'denied', 'expired')),
    approved_by  UUID        REFERENCES users(id),
    approved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_remediation_approvals_tenant_status  ON remediation_approvals (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_remediation_approvals_expires_at     ON remediation_approvals (expires_at);
CREATE INDEX IF NOT EXISTS idx_remediation_approvals_node           ON remediation_approvals (node_id);

COMMENT ON TABLE  remediation_approvals IS 'Pending/resolved operator approvals for high-severity auto-remediations';
COMMENT ON COLUMN remediation_approvals.task_payload IS 'Serialised task payload used to re-dispatch after approval (bypasses severity gate)';

CREATE TABLE IF NOT EXISTS remediation_circuit_breaker_state (
    tenant_id      UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_id        TEXT        NOT NULL,
    tripped_at     TIMESTAMPTZ NOT NULL,
    tripped_reason TEXT        NOT NULL,
    acked_at       TIMESTAMPTZ,
    acked_by       UUID        REFERENCES users(id),
    PRIMARY KEY (tenant_id, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_remediation_circuit_breaker_tripped
    ON remediation_circuit_breaker_state (tripped_at)
    WHERE acked_at IS NULL;

COMMENT ON TABLE remediation_circuit_breaker_state IS 'Per (tenant, rule) circuit-breaker trip state; unacked trips short-circuit new remediations';
