-- Pillar 1.7/1.8: node lifecycle (heartbeat + first-scan gating + labels).
-- Adds three columns to nodes that the enrollment UI + remediation/rollout
-- safety rails read later this sprint:
--   * last_seen_at  — bumped by /api/v1/nodes/:id/heartbeat; used by rollout
--                     health gates and staleness detection.
--   * first_scan_at — set once when a node's first compliance scan lands;
--                     combined with a heartbeat it flips state from
--                     enrollment_pending to active.
--   * labels        — free-form JSONB map. Worktree C reads
--                     labels['remediation']='manual-only' as an opt-out, and
--                     Worktree E propagates cluster.* keys from the cluster
--                     labels map.
--
-- Also expands the state CHECK constraint to cover the new enrollment
-- lifecycle values (enrollment_pending + enrollment_failed). The 0022
-- migration introduced the column as a plain TEXT without a CHECK — we
-- preserve idempotency with the DROP IF EXISTS below so re-running on a
-- tree that has a legacy constraint does not fail.

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS first_scan_at TIMESTAMPTZ;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS labels JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS nodes_state_check;
ALTER TABLE nodes ADD CONSTRAINT nodes_state_check
    CHECK (state IN ('enrollment_pending','active','enrollment_failed','retired'));

CREATE INDEX IF NOT EXISTS idx_nodes_last_seen_at ON nodes (last_seen_at);
CREATE INDEX IF NOT EXISTS idx_nodes_labels_gin ON nodes USING GIN (labels);
