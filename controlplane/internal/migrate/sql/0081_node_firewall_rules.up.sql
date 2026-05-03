-- node_firewall_rules tracks per-node firewall rules dispatched in response to
-- operator-driven entity_actions (block IP X). One entity_action fans out to
-- N rows here — one per affected node. Rows are mutated as the agent reports
-- back via heartbeat completed_actions: pending → applied | failed | removed.

CREATE TABLE IF NOT EXISTS node_firewall_rules (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_action_id  UUID         NOT NULL REFERENCES entity_actions(id) ON DELETE CASCADE,
    node_id           UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tenant_id         UUID         NOT NULL,
    action            TEXT         NOT NULL CHECK (action IN ('block','allow')),
    direction         TEXT         NOT NULL DEFAULT 'in' CHECK (direction IN ('in','out')),
    protocol          TEXT,                                 -- tcp | udp | icmp | NULL=any
    port              INTEGER,                              -- 0 / NULL = any
    source            TEXT,                                 -- IP or CIDR; the IP we're blocking
    dest              TEXT,                                 -- IP or CIDR; usually NULL
    tag               TEXT         NOT NULL,                -- c1-{entity_action_id}; lets us find/remove our own rules
    status            TEXT         NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applied','failed','removed')),
    error             TEXT,
    job_id            UUID,                                 -- the firewall.rule_add or firewall.rule_delete job that owns the dispatch
    requested_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    applied_at        TIMESTAMPTZ,
    removed_at        TIMESTAMPTZ,
    UNIQUE (entity_action_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_node_firewall_rules_node
    ON node_firewall_rules (node_id, status);
CREATE INDEX IF NOT EXISTS idx_node_firewall_rules_entity_action
    ON node_firewall_rules (entity_action_id);
CREATE INDEX IF NOT EXISTS idx_node_firewall_rules_tenant
    ON node_firewall_rules (tenant_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_node_firewall_rules_tag
    ON node_firewall_rules (tag);
