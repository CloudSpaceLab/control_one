DROP INDEX IF EXISTS idx_nodes_agent_version;
ALTER TABLE nodes DROP COLUMN IF EXISTS agent_version;
