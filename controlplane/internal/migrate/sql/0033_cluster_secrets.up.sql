CREATE TABLE IF NOT EXISTS cluster_secrets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, key)
);

CREATE INDEX IF NOT EXISTS idx_cluster_secrets_cluster ON cluster_secrets (cluster_id);

-- Per-node delivery state for cluster-scoped secrets. A row exists for each
-- (cluster, node, key) that the control plane has pushed (or attempted to
-- push) to a cluster member. Agents read their slice of this table via
-- /api/v1/secrets/sync; operators read the whole table via the cluster
-- secret API for audit. Rows with action='delete' mark tombstones so the
-- agent can unset the value on next poll.
CREATE TABLE IF NOT EXISTS cluster_secret_node_state (
    cluster_id  UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    node_id     UUID NOT NULL REFERENCES nodes(id)    ON DELETE CASCADE,
    key         TEXT NOT NULL,
    action      TEXT NOT NULL DEFAULT 'upsert',
    sync_status TEXT NOT NULL DEFAULT 'pending',
    pushed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (cluster_id, node_id, key)
);

CREATE INDEX IF NOT EXISTS idx_cluster_secret_node_state_node ON cluster_secret_node_state (node_id);
CREATE INDEX IF NOT EXISTS idx_cluster_secret_node_state_cluster_key ON cluster_secret_node_state (cluster_id, key);
