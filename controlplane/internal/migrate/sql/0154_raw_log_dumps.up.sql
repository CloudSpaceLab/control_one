-- Raw log dump requests and resumable agent uploads.
-- Payload bytes live only in the restricted artifact directory; the database
-- stores lifecycle, integrity and scope metadata.
CREATE TABLE IF NOT EXISTS agent_log_dumps (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id             UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    job_id              UUID REFERENCES jobs(id) ON DELETE SET NULL,
    source              TEXT NOT NULL CHECK (source IN ('control_plane', 'node_agent')),
    entity_filter       JSONB NOT NULL DEFAULT '{}'::jsonb,
    window_start        TIMESTAMPTZ NOT NULL,
    window_end          TIMESTAMPTZ NOT NULL,
    retention_days      SMALLINT NOT NULL DEFAULT 7 CHECK (retention_days IN (1, 3, 7, 14, 30)),
    status              TEXT NOT NULL DEFAULT 'requested'
                        CHECK (status IN ('requested', 'capturing', 'captured', 'failed', 'expired', 'deleting')),
    artifact_path       TEXT,
    artifact_sha256     TEXT,
    row_count           BIGINT NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    size_bytes          BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    truncated           BOOLEAN NOT NULL DEFAULT FALSE,
    source_available    BOOLEAN,
    source_reason       TEXT,
    error               TEXT,
    requested_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    claim_token_sha256  TEXT,
    claim_generation    BIGINT NOT NULL DEFAULT 0 CHECK (claim_generation >= 0),
    claimed_at          TIMESTAMPTZ,
    claim_expires_at    TIMESTAMPTZ,
    capture_started_at  TIMESTAMPTZ,
    captured_at         TIMESTAMPTZ,
    cleanup_attempts    INTEGER NOT NULL DEFAULT 0 CHECK (cleanup_attempts >= 0),
    cleanup_error       TEXT,
    next_cleanup_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ NOT NULL,
    CONSTRAINT agent_log_dumps_window CHECK (window_end > window_start),
    CONSTRAINT agent_log_dumps_artifact_sha256 CHECK (artifact_sha256 IS NULL OR artifact_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT agent_log_dumps_claim_sha256 CHECK (claim_token_sha256 IS NULL OR claim_token_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT agent_log_dumps_scope_unique UNIQUE (id, tenant_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_tenant_created
    ON agent_log_dumps (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_node_created
    ON agent_log_dumps (tenant_id, node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_job
    ON agent_log_dumps (job_id)
    WHERE job_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_pending_agent
    ON agent_log_dumps (node_id, created_at)
    WHERE source = 'node_agent' AND status IN ('requested', 'capturing');
CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_expiry
    ON agent_log_dumps (expires_at)
    WHERE status IN ('requested', 'capturing', 'captured', 'failed', 'expired', 'deleting');
CREATE INDEX IF NOT EXISTS idx_agent_log_dumps_claim_expiry
    ON agent_log_dumps (claim_expires_at)
    WHERE status = 'capturing';

CREATE TABLE IF NOT EXISTS agent_log_dump_chunks (
    dump_id      UUID NOT NULL,
    tenant_id    UUID NOT NULL,
    node_id      UUID NOT NULL,
    job_id       UUID REFERENCES jobs(id) ON DELETE SET NULL,
    ordinal      INTEGER NOT NULL CHECK (ordinal >= 0),
    sha256       TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes   BIGINT NOT NULL CHECK (size_bytes >= 0),
    temp_path    TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (dump_id, ordinal),
    CONSTRAINT agent_log_dump_chunks_scope_fk
        FOREIGN KEY (dump_id, tenant_id, node_id)
        REFERENCES agent_log_dumps (id, tenant_id, node_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agent_log_dump_chunks_scope
    ON agent_log_dump_chunks (tenant_id, node_id, dump_id, ordinal);
