CREATE TABLE IF NOT EXISTS rule_trigger_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_id       UUID NOT NULL,
    rule_type     TEXT NOT NULL CHECK (rule_type IN ('port','log','compliance')),
    node_id       UUID REFERENCES nodes(id) ON DELETE SET NULL,
    severity      TEXT NOT NULL DEFAULT 'medium',
    details       JSONB NOT NULL DEFAULT '{}'::jsonb,
    triggered_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rule_trig_tenant_time ON rule_trigger_log (tenant_id, triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_rule_trig_rule        ON rule_trigger_log (rule_id);
CREATE INDEX IF NOT EXISTS idx_rule_trig_node        ON rule_trigger_log (node_id);
CREATE INDEX IF NOT EXISTS idx_rule_trig_type        ON rule_trigger_log (rule_type);
