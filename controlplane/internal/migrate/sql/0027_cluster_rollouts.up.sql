CREATE TABLE IF NOT EXISTS cluster_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    template_version_id UUID NOT NULL REFERENCES provisioning_template_versions(id),
    wave_size INTEGER NOT NULL DEFAULT 1 CHECK (wave_size >= 1),
    wave_strategy TEXT NOT NULL DEFAULT 'rolling',
    health_gate JSONB NOT NULL DEFAULT '{}'::jsonb,
    state TEXT NOT NULL DEFAULT 'pending',
    current_wave INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cluster_rollouts_cluster ON cluster_rollouts (cluster_id);
CREATE INDEX IF NOT EXISTS idx_cluster_rollouts_state ON cluster_rollouts (state);
