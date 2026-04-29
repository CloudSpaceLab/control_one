-- Sprint 1: persist the agent binary version reported on every heartbeat so
-- operators can see at a glance which nodes are running stale agents and
-- trigger self-update from the UI.

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS agent_version VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_nodes_agent_version ON nodes(agent_version)
  WHERE agent_version IS NOT NULL;
