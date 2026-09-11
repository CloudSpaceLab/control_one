CREATE TABLE IF NOT EXISTS metric_threshold_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    metric_name     TEXT        NOT NULL,
    operator        TEXT        NOT NULL CHECK (operator IN ('gt', 'gte', 'lt', 'lte', 'eq')),
    threshold       DOUBLE PRECISION NOT NULL,
    window_seconds  INTEGER     NOT NULL DEFAULT 300,
    severity        TEXT        NOT NULL DEFAULT 'warning'
                                CHECK (severity IN ('info', 'warning', 'critical')),
    action          TEXT        NOT NULL DEFAULT 'alert'
                                CHECK (action IN ('alert', 'notify', 'auto_remediate')),
    target_node_id  UUID REFERENCES nodes(id) ON DELETE SET NULL,
    enabled         BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_metric_threshold_rules_tenant
    ON metric_threshold_rules (tenant_id, enabled);

CREATE INDEX IF NOT EXISTS idx_metric_threshold_rules_metric
    ON metric_threshold_rules (metric_name, enabled);
