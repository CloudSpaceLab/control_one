CREATE TABLE IF NOT EXISTS cluster_members (
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    position INTEGER NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (cluster_id, node_id),
    UNIQUE (cluster_id, role, position)
);

CREATE INDEX IF NOT EXISTS idx_cluster_members_node ON cluster_members (node_id);
