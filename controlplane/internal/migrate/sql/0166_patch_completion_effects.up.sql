-- Acknowledgment is safe only after receipt and deployment rollup commit.
CREATE TABLE patch_completion_effects (
    job_id UUID PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
