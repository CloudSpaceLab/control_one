DROP INDEX IF EXISTS idx_event_ingest_batches_retry_due;

CREATE INDEX IF NOT EXISTS idx_event_ingest_batches_retry_due
    ON event_ingest_batches (status, next_attempt_at, received_at)
    WHERE status IN ('received', 'pending_doris');
