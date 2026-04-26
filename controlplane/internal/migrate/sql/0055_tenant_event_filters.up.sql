-- Per-tenant capture-filter policy. Pushed to agents via heartbeat response;
-- mirrors the tenant_remediation_config pattern.

CREATE TABLE IF NOT EXISTS tenant_event_filters (
    tenant_id                  UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    capture_external           BOOLEAN NOT NULL DEFAULT TRUE,
    capture_internal_summary   BOOLEAN NOT NULL DEFAULT TRUE,
    capture_listening_changes  BOOLEAN NOT NULL DEFAULT TRUE,
    capture_files              BOOLEAN NOT NULL DEFAULT TRUE,
    capture_db_queries         BOOLEAN NOT NULL DEFAULT TRUE,
    threat_match_full          BOOLEAN NOT NULL DEFAULT TRUE,
    file_paths_watch           TEXT[] NOT NULL DEFAULT ARRAY['/etc/','/var/lib/','/var/log/','/opt/','/home/','/root/'],
    file_size_min_bytes        BIGINT NOT NULL DEFAULT 0,
    allowlist_cidrs            TEXT[] NOT NULL DEFAULT '{}',
    denylist_cidrs             TEXT[] NOT NULL DEFAULT '{}',
    db_query_text_capture      BOOLEAN NOT NULL DEFAULT TRUE,
    forensic_mode              BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenant_event_filters_forensic
    ON tenant_event_filters (tenant_id) WHERE forensic_mode = TRUE;
