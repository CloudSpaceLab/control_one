CREATE TABLE IF NOT EXISTS fleet_enrollment_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 22,
    success BOOLEAN NOT NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    error_message TEXT,
    ssh_output TEXT,
    duration_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_fleet_results_job ON fleet_enrollment_results(job_id);
