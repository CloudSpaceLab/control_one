-- Reverse of 0028_node_lifecycle.up.sql. Drops the GIN/btree indexes, the
-- enumerated state CHECK, and the three added columns. The CHECK expression
-- is restored to its pre-0028 permissive form (no constraint) which matches
-- what migration 0022 originally shipped.

DROP INDEX IF EXISTS idx_nodes_labels_gin;
DROP INDEX IF EXISTS idx_nodes_last_seen_at;

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS nodes_state_check;

ALTER TABLE nodes DROP COLUMN IF EXISTS labels;
ALTER TABLE nodes DROP COLUMN IF EXISTS first_scan_at;
ALTER TABLE nodes DROP COLUMN IF EXISTS last_seen_at;
