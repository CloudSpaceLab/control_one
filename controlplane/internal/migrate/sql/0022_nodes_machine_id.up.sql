-- Pillar 1.4: machine-id idempotent enrollment.
-- Adds a stable, OS-derived machine identifier column to nodes so that
-- re-runs of the installer on the same physical/virtual host don't create
-- duplicate node rows. The partial unique index enforces uniqueness per
-- tenant only when a machine_id is actually set (NULL allowed for legacy
-- rows that enrolled before the installer was updated).

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS machine_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_tenant_machine_id
  ON nodes (tenant_id, machine_id) WHERE machine_id IS NOT NULL;

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS state TEXT NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_nodes_state ON nodes (state);
