ALTER TABLE event_ingest_batches
    ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_event_ingest_batches_retry_due
    ON event_ingest_batches (status, next_attempt_at, received_at)
    WHERE status IN ('received', 'pending_doris');

CREATE INDEX IF NOT EXISTS idx_event_ingest_batches_last_error
    ON event_ingest_batches (last_error_at DESC)
    WHERE status IN ('pending_doris', 'failed') AND error_message IS NOT NULL;
