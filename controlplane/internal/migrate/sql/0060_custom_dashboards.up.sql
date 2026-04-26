-- Custom dashboards builder.
--
-- A dashboard is a named collection of widgets. Each widget pulls from one
-- or more nodes (or all nodes in a tenant) and renders one of the four
-- supported types: db_query, sys_resources, log_size, network_bytes.
--
-- spec is JSONB so widget params evolve without schema migrations:
--   db_query     : { engine, target_name, query_hash | query_pattern }
--   sys_resources: { metric: cpu | mem | disk, range: 1h | 24h | 7d }
--   log_size     : { source_program?, range }
--   network_bytes: { direction: in | out | both, range }
--
-- node_ids = []  → "all nodes in this tenant"
-- shared   = false → owner-only; true → readable by anyone with
--                    dashboards.read in the tenant.

CREATE TABLE IF NOT EXISTS custom_dashboards (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    layout      JSONB NOT NULL DEFAULT '{}'::jsonb,
    shared      BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS custom_dashboards_tenant_idx ON custom_dashboards (tenant_id);
CREATE INDEX IF NOT EXISTS custom_dashboards_owner_idx  ON custom_dashboards (owner_id);

CREATE TABLE IF NOT EXISTS custom_dashboard_widgets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id    UUID NOT NULL REFERENCES custom_dashboards(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    widget_type     TEXT NOT NULL CHECK (widget_type IN ('db_query','sys_resources','log_size','network_bytes')),
    spec            JSONB NOT NULL DEFAULT '{}'::jsonb,
    node_ids        UUID[] NOT NULL DEFAULT '{}'::uuid[],
    refresh_seconds INTEGER NOT NULL DEFAULT 30,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS custom_dashboard_widgets_dashboard_idx
    ON custom_dashboard_widgets (dashboard_id, sort_order);
