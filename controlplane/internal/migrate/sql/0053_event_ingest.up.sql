-- Persist-first ingest journal + per-hour rollup. Both are small Postgres
-- tables; raw events live in Doris (when configured) or are aggregated only.
-- The journal lets a controlplane replica restart mid-flush without dropping
-- a batch — Doris fan-out resumes from the journal.

CREATE TABLE IF NOT EXISTS event_ingest_batches (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID,
    node_id         UUID,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    rows            INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'received'
                        CHECK (status IN ('received','accepted','pending_doris','failed','archived')),
    doris_status    TEXT,
    last_attempt_at TIMESTAMPTZ,
    payload         BYTEA,
    error_message   TEXT
);
CREATE INDEX IF NOT EXISTS idx_event_ingest_batches_status_received
    ON event_ingest_batches (status, received_at);
CREATE INDEX IF NOT EXISTS idx_event_ingest_batches_tenant
    ON event_ingest_batches (tenant_id);

CREATE TABLE IF NOT EXISTS event_rollups_hourly (
    tenant_id   UUID NOT NULL,
    node_id     UUID,
    event_type  TEXT NOT NULL,
    hour_ts     TIMESTAMPTZ NOT NULL,
    cnt         BIGINT NOT NULL DEFAULT 0,
    bytes_in    BIGINT NOT NULL DEFAULT 0,
    bytes_out   BIGINT NOT NULL DEFAULT 0,
    sev_max     TEXT,
    PRIMARY KEY (tenant_id, node_id, event_type, hour_ts)
);
CREATE INDEX IF NOT EXISTS idx_event_rollups_tenant_hour
    ON event_rollups_hourly (tenant_id, hour_ts DESC);
