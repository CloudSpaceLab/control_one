-- node_services stores the listening service inventory reported by the agent.
-- One row per (node_id, listen_addr, port, pid). Replaced atomically when the
-- agent reports a fresh inventory cycle (every ~10 minutes by default).
--
-- service_kind is the heuristic fingerprint (nginx, postgres, redis, etc) the
-- agent computes locally. Probe fields are populated only when the optional
-- localhost HTTP probe is enabled (off by default). The knowledge-graph
-- markdown generator reads this table per-tenant + the existing
-- node_packages / node_firewall_state tables to synthesize a CISO-friendly
-- document an LLM can answer questions against.

CREATE TABLE IF NOT EXISTS node_services (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id           UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tenant_id         UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pid               INTEGER     NOT NULL DEFAULT 0,
    process           TEXT        NOT NULL DEFAULT '',
    binary_path       TEXT        NOT NULL DEFAULT '',
    listen_addr       TEXT        NOT NULL DEFAULT '',
    port              INTEGER     NOT NULL,
    service_kind      TEXT        NOT NULL DEFAULT 'unknown',
    probe_status      INTEGER,
    probe_server      TEXT,
    probe_title       TEXT,
    probe_content_type TEXT,
    observed_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_node_services_node     ON node_services(node_id);
CREATE INDEX IF NOT EXISTS idx_node_services_tenant   ON node_services(tenant_id);
CREATE INDEX IF NOT EXISTS idx_node_services_kind     ON node_services(service_kind);
CREATE INDEX IF NOT EXISTS idx_node_services_port     ON node_services(port);
