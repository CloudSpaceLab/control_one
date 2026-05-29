DROP INDEX IF EXISTS idx_event_ingest_batches_replay_key;

ALTER TABLE event_ingest_batches
    DROP COLUMN IF EXISTS replay_key;
