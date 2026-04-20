-- Pillar 1.9: 90-day client cert rotation.
-- Adds tracking columns to nodes so we know the active client-cert serial and
-- when it was last rotated, plus a history table chained via `replaced_by` so
-- we can audit the full rotation lineage per node.

ALTER TABLE nodes
    ADD COLUMN IF NOT EXISTS cert_serial     TEXT,
    ADD COLUMN IF NOT EXISTS cert_rotated_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS node_certificate_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id     UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    serial      TEXT NOT NULL,
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    replaced_by UUID REFERENCES node_certificate_history(id)
);

CREATE INDEX IF NOT EXISTS idx_node_cert_history_node ON node_certificate_history (node_id);
