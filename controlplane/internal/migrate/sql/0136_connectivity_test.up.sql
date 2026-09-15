CREATE TABLE IF NOT EXISTS node_connectivity_tests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id       UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tenant_id     UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    job_id        UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    target_ip     TEXT        NOT NULL,
    target_port   INTEGER     NOT NULL,
    protocol      TEXT        NOT NULL DEFAULT 'tcp',
    timeout_ms    INTEGER     NOT NULL DEFAULT 5000,
    status        TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    reachable     BOOLEAN,
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_node_connectivity_tests_node
    ON node_connectivity_tests (node_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_node_connectivity_tests_job
    ON node_connectivity_tests (job_id)
    WHERE job_id IS NOT NULL;
