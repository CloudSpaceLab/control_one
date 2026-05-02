-- node_health_scores stores the latest predictive health score per node.
-- The score is recomputed hourly by the JobTypeHealthPredict job which
-- aggregates telemetry signals (SMART, swap, iowait, OOM, packet loss,
-- ICMP latency) and compares against EWMA baselines stored in the
-- generic behavioral_baselines table (signal_type='health.<metric>').
--
-- Hysteresis + cold-start gating live in the predict job; this table
-- only reflects the latest snapshot. risk_level is one of:
--   - calibrating  : insufficient samples (<24 per metric)
--   - low          : score >= 75
--   - medium       : 50 <= score < 75
--   - high         : 25 <= score < 50
--   - critical     : score < 25
--
-- components is the per-signal penalty breakdown plus contextual hints
-- (largest contributor → primary_component for incident dedupe). For
-- calibrating rows, components.calibrating_samples reports the lowest
-- per-metric sample count so the UI can render "N/24 samples" honestly
-- instead of fake zeros.

CREATE TABLE IF NOT EXISTS node_health_scores (
    node_id     UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    score       INTEGER NOT NULL DEFAULT 100 CHECK (score BETWEEN 0 AND 100),
    risk_level  TEXT NOT NULL DEFAULT 'calibrating',
    components  JSONB NOT NULL DEFAULT '{}'::jsonb,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fleet-sort index — ListAtRisk orders by score ASC.
CREATE INDEX IF NOT EXISTS idx_node_health_scores_score ON node_health_scores (score);
