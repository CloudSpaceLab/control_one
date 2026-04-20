CREATE TABLE IF NOT EXISTS cluster_lb_registrations (
    cluster_id      UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    node_id         UUID NOT NULL REFERENCES nodes(id)    ON DELETE CASCADE,
    provider        TEXT NOT NULL,
    lb_identifier   TEXT NOT NULL,
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deregistered_at TIMESTAMPTZ,
    PRIMARY KEY (cluster_id, node_id, lb_identifier)
);

CREATE INDEX IF NOT EXISTS idx_cluster_lb_registrations_node ON cluster_lb_registrations (node_id);
CREATE INDEX IF NOT EXISTS idx_cluster_lb_registrations_cluster ON cluster_lb_registrations (cluster_id);
