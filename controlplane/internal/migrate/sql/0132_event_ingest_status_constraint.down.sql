UPDATE event_ingest_batches
   SET status = 'received'
 WHERE status = 'local_completed';

ALTER TABLE event_ingest_batches
    DROP CONSTRAINT IF EXISTS event_ingest_batches_status_check;

ALTER TABLE event_ingest_batches
    ADD CONSTRAINT event_ingest_batches_status_check
    CHECK (status IN ('received', 'accepted', 'pending_doris', 'failed', 'archived'));
