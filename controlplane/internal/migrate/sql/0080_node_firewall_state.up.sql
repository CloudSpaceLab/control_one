-- node_firewall_state stores the per-node firewall snapshot reported by the
-- agent on every heartbeat (small payload — sent in full each time, no delta).
-- One row per node; upserted on each heartbeat that includes firewall_state.

CREATE TABLE IF NOT EXISTS node_firewall_state (
    node_id        UUID        PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    firewall_type  TEXT        NOT NULL, -- ufw | firewalld | iptables | nftables | windows_defender_firewall | none
    enabled        BOOLEAN     NOT NULL DEFAULT FALSE,
    rules          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    zones          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    raw            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_node_firewall_state_type ON node_firewall_state(firewall_type);
