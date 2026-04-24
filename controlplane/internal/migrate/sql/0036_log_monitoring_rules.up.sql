CREATE TABLE IF NOT EXISTS log_monitoring_rules (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    policy_id        UUID REFERENCES policies(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    log_source       TEXT NOT NULL,
    pattern          TEXT NOT NULL,
    severity         TEXT NOT NULL DEFAULT 'medium',
    window_seconds   INTEGER NOT NULL DEFAULT 60 CHECK (window_seconds > 0),
    threshold        INTEGER NOT NULL DEFAULT 1 CHECK (threshold > 0),
    action           TEXT NOT NULL DEFAULT 'notify',
    target_labels    JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_log_rules_tenant  ON log_monitoring_rules (tenant_id);
CREATE INDEX IF NOT EXISTS idx_log_rules_policy  ON log_monitoring_rules (policy_id);
CREATE INDEX IF NOT EXISTS idx_log_rules_source  ON log_monitoring_rules (log_source);
CREATE INDEX IF NOT EXISTS idx_log_rules_enabled ON log_monitoring_rules (enabled);
