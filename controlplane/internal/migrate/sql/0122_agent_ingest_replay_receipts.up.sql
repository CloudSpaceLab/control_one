CREATE TABLE IF NOT EXISTS agent_ingest_replay_receipts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    node_id       UUID NOT NULL,
    endpoint      TEXT NOT NULL,
    replay_key    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'accepted'
                      CHECK (status IN ('accepted','failed')),
    response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, node_id, endpoint, replay_key)
);

CREATE INDEX IF NOT EXISTS idx_agent_ingest_replay_receipts_scope
    ON agent_ingest_replay_receipts (tenant_id, node_id, endpoint, updated_at DESC);
