CREATE TABLE node_update_candidate_observations (
 id UUID PRIMARY KEY,
 node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 snapshot_sha256 TEXT NOT NULL,
 observed_at TIMESTAMPTZ NOT NULL,
 observations JSONB NOT NULL,
 UNIQUE(node_id,snapshot_sha256,observed_at)
);
CREATE INDEX node_update_candidate_observations_latest ON node_update_candidate_observations(node_id,observed_at DESC);
