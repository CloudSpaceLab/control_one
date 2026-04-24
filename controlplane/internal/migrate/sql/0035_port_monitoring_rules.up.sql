CREATE TABLE IF NOT EXISTS port_monitoring_rules (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    policy_id        UUID REFERENCES policies(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    port             INTEGER NOT NULL CHECK (port > 0 AND port < 65536),
    protocol         TEXT NOT NULL DEFAULT 'tcp' CHECK (protocol IN ('tcp','udp')),
    expected_state   TEXT NOT NULL CHECK (expected_state IN ('open','closed')),
    target_labels    JSONB NOT NULL DEFAULT '{}'::jsonb,
    severity         TEXT NOT NULL DEFAULT 'medium',
    action           TEXT NOT NULL DEFAULT 'notify',
    enabled          BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_port_rules_tenant  ON port_monitoring_rules (tenant_id);
CREATE INDEX IF NOT EXISTS idx_port_rules_policy  ON port_monitoring_rules (policy_id);
CREATE INDEX IF NOT EXISTS idx_port_rules_enabled ON port_monitoring_rules (enabled);
