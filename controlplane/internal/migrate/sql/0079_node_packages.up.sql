-- node_packages stores the OS-package inventory reported by the agent during
-- heartbeats. The agent only resends the full list when its computed hash
-- changes or once every 24h; node_inventory_sync tracks the last-known-good
-- hash + sync timestamp so the server can ask for a full re-sync when needed.

CREATE TABLE IF NOT EXISTS node_packages (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id      UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    version      TEXT        NOT NULL,
    source       TEXT        NOT NULL, -- apt | dpkg | rpm | winget | other
    arch         TEXT,
    installed_at TIMESTAMPTZ,
    observed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (node_id, name, version, arch)
);

CREATE INDEX IF NOT EXISTS idx_node_packages_node ON node_packages(node_id);
CREATE INDEX IF NOT EXISTS idx_node_packages_name ON node_packages(name);

CREATE TABLE IF NOT EXISTS node_inventory_sync (
    node_id        UUID        PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    package_hash   TEXT        NOT NULL,
    package_count  INTEGER     NOT NULL DEFAULT 0,
    kernel_version TEXT,
    os_version     TEXT,
    last_full_sync TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
