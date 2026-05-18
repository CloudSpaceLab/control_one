WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY tenant_id,
                            COALESCE(node_id, '00000000-0000-0000-0000-000000000000'::uuid),
                            signal_type,
                            dimension
               ORDER BY computed_at DESC, id DESC
           ) AS rn
    FROM behavioral_baselines
)
DELETE FROM behavioral_baselines
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_behavioral_baselines_global
    ON behavioral_baselines (tenant_id, signal_type, dimension)
    WHERE node_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_behavioral_baselines_node
    ON behavioral_baselines (tenant_id, node_id, signal_type, dimension)
    WHERE node_id IS NOT NULL;
