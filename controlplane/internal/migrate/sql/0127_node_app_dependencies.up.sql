CREATE TABLE IF NOT EXISTS node_app_dependencies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id         UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    app_root        TEXT NOT NULL DEFAULT '',
    ecosystem       TEXT NOT NULL,
    name            TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT '',
    package_manager TEXT NOT NULL DEFAULT '',
    manifest_path   TEXT NOT NULL DEFAULT '',
    scope           TEXT NOT NULL DEFAULT '',
    license         TEXT NOT NULL DEFAULT '',
    purl            TEXT NOT NULL DEFAULT '',
    cpe             TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (node_id, app_root, ecosystem, name, version, manifest_path)
);

CREATE INDEX IF NOT EXISTS idx_node_app_dependencies_node
    ON node_app_dependencies(node_id);

CREATE INDEX IF NOT EXISTS idx_node_app_dependencies_tenant_name
    ON node_app_dependencies(tenant_id, ecosystem, name);

CREATE INDEX IF NOT EXISTS idx_node_app_dependencies_purl
    ON node_app_dependencies(purl)
    WHERE purl <> '';
