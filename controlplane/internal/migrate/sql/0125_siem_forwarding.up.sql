CREATE TABLE IF NOT EXISTS siem_forwarding_destinations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name               TEXT        NOT NULL,
    kind               TEXT        NOT NULL CHECK (kind IN ('loki', 'elasticsearch', 'splunk_hec', 'sentinel')),
    status             TEXT        NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    url                TEXT        NOT NULL,
    config             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by_subject TEXT        NOT NULL DEFAULT '',
    updated_by_subject TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_siem_forwarding_destinations_tenant_status
    ON siem_forwarding_destinations (tenant_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS siem_forwarding_checkpoints (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    destination_id     UUID        NOT NULL REFERENCES siem_forwarding_destinations(id) ON DELETE CASCADE,
    cursor_at          TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    cursor_log_id      UUID,
    last_record_at     TIMESTAMPTZ,
    last_success_at    TIMESTAMPTZ,
    last_error         TEXT        NOT NULL DEFAULT '',
    records_forwarded  BIGINT      NOT NULL DEFAULT 0 CHECK (records_forwarded >= 0),
    batches_forwarded  BIGINT      NOT NULL DEFAULT 0 CHECK (batches_forwarded >= 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, destination_id)
);

CREATE INDEX IF NOT EXISTS idx_siem_forwarding_checkpoints_tenant_cursor
    ON siem_forwarding_checkpoints (tenant_id, cursor_at DESC);

CREATE TABLE IF NOT EXISTS siem_forwarding_delivery_attempts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    destination_id     UUID        NOT NULL REFERENCES siem_forwarding_destinations(id) ON DELETE CASCADE,
    status             TEXT        NOT NULL CHECK (status IN ('succeeded', 'failed')),
    record_count       INTEGER     NOT NULL DEFAULT 0 CHECK (record_count >= 0),
    batch_start_at     TIMESTAMPTZ,
    batch_end_at       TIMESTAMPTZ,
    error              TEXT        NOT NULL DEFAULT '',
    details            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    attempted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_siem_forwarding_attempts_destination_time
    ON siem_forwarding_delivery_attempts (tenant_id, destination_id, attempted_at DESC);

COMMENT ON TABLE siem_forwarding_destinations IS 'Tenant-scoped outbound SIEM forwarding destinations for coexistence and migration';
COMMENT ON TABLE siem_forwarding_checkpoints IS 'Per-destination telemetry log forwarding cursors and counters';
COMMENT ON COLUMN siem_forwarding_checkpoints.cursor_log_id IS 'Last telemetry_logs.id forwarded at cursor_at, used with cursor_at as a stable batch cursor';
COMMENT ON TABLE siem_forwarding_delivery_attempts IS 'Auditable outbound SIEM forwarding delivery attempts';
