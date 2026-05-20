DROP INDEX IF EXISTS idx_event_ingest_batches_last_error;
DROP INDEX IF EXISTS idx_event_ingest_batches_retry_due;

ALTER TABLE event_ingest_batches
    DROP COLUMN IF EXISTS last_error_at,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS retry_count;
