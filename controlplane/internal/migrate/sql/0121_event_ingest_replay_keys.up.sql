ALTER TABLE event_ingest_batches
    ADD COLUMN IF NOT EXISTS replay_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_event_ingest_batches_replay_key
    ON event_ingest_batches (
        COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(node_id, '00000000-0000-0000-0000-000000000000'::uuid),
        replay_key
    )
    WHERE replay_key <> '';
