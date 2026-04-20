CREATE TABLE IF NOT EXISTS cluster_rollout_waves (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rollout_id   UUID NOT NULL REFERENCES cluster_rollouts(id) ON DELETE CASCADE,
    wave_number  INTEGER NOT NULL,
    member_ids   UUID[] NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    state        TEXT NOT NULL DEFAULT 'running',
    gate_result  JSONB,
    UNIQUE (rollout_id, wave_number)
);

CREATE INDEX IF NOT EXISTS idx_cluster_rollout_waves_rollout ON cluster_rollout_waves (rollout_id);
