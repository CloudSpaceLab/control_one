CREATE TABLE IF NOT EXISTS ip_behavior_rollups (
    tenant_id      UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id        UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    hour_ts        TIMESTAMPTZ NOT NULL,
    server_group   TEXT        NOT NULL DEFAULT '',
    app            TEXT        NOT NULL DEFAULT '',
    country_code   TEXT        NOT NULL DEFAULT '',
    country        TEXT        NOT NULL DEFAULT '',
    asn            TEXT        NOT NULL DEFAULT '',
    isp            TEXT        NOT NULL DEFAULT '',
    src_ip         INET        NOT NULL,
    request_count  BIGINT      NOT NULL DEFAULT 0,
    bytes_out      BIGINT      NOT NULL DEFAULT 0,
    status_301     BIGINT      NOT NULL DEFAULT 0,
    status_401     BIGINT      NOT NULL DEFAULT 0,
    status_403     BIGINT      NOT NULL DEFAULT 0,
    status_404     BIGINT      NOT NULL DEFAULT 0,
    status_429     BIGINT      NOT NULL DEFAULT 0,
    status_500     BIGINT      NOT NULL DEFAULT 0,
    status_502     BIGINT      NOT NULL DEFAULT 0,
    status_503     BIGINT      NOT NULL DEFAULT 0,
    status_2xx     BIGINT      NOT NULL DEFAULT 0,
    status_3xx     BIGINT      NOT NULL DEFAULT 0,
    status_4xx     BIGINT      NOT NULL DEFAULT 0,
    status_5xx     BIGINT      NOT NULL DEFAULT 0,
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, node_id, hour_ts, server_group, app, country_code, asn, src_ip)
);

CREATE INDEX IF NOT EXISTS idx_ip_behavior_rollups_tenant_hour
    ON ip_behavior_rollups (tenant_id, hour_ts DESC);
CREATE INDEX IF NOT EXISTS idx_ip_behavior_rollups_country
    ON ip_behavior_rollups (tenant_id, country_code, hour_ts DESC);
CREATE INDEX IF NOT EXISTS idx_ip_behavior_rollups_src_ip
    ON ip_behavior_rollups (tenant_id, src_ip, hour_ts DESC);

CREATE TABLE IF NOT EXISTS ip_behavior_findings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID        REFERENCES nodes(id) ON DELETE SET NULL,
    dedup_key     TEXT        NOT NULL,
    src_ip        INET,
    country_code  TEXT        NOT NULL DEFAULT '',
    asn           TEXT        NOT NULL DEFAULT '',
    category      TEXT        NOT NULL DEFAULT 'ip_behavior',
    severity      TEXT        NOT NULL DEFAULT 'medium',
    score         INTEGER     NOT NULL DEFAULT 0,
    status        TEXT        NOT NULL DEFAULT 'open'
                              CHECK (status IN ('open','acknowledged','contained','resolved','suppressed')),
    reason        TEXT        NOT NULL DEFAULT '',
    evidence      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, dedup_key)
);

CREATE INDEX IF NOT EXISTS idx_ip_behavior_findings_tenant_status
    ON ip_behavior_findings (tenant_id, status, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_ip_behavior_findings_src_ip
    ON ip_behavior_findings (tenant_id, src_ip, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS ip_blocklist_entries (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    finding_id        UUID        REFERENCES ip_behavior_findings(id) ON DELETE SET NULL,
    ip_cidr           TEXT        NOT NULL,
    scope             TEXT        NOT NULL DEFAULT 'node',
    target_type       TEXT        NOT NULL DEFAULT 'node',
    target_id         UUID,
    server_group      TEXT        NOT NULL DEFAULT '',
    app               TEXT        NOT NULL DEFAULT '',
    vhost             TEXT        NOT NULL DEFAULT '',
    enforcement       TEXT        NOT NULL DEFAULT 'firewall',
    protected_override BOOLEAN    NOT NULL DEFAULT FALSE,
    protected_override_reason TEXT NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'proposed'
                                  CHECK (status IN ('proposed','approved','canary','dispatching','active','failed','expired','removed','denied','rejected','rolled_back')),
    reason            TEXT        NOT NULL DEFAULT '',
    score             INTEGER     NOT NULL DEFAULT 0,
    expires_at        TIMESTAMPTZ,
    approved_by       UUID,
    approved_at       TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ip_blocklist_entries_tenant_status
    ON ip_blocklist_entries (tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS webserver_instances (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id         UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,
    version         TEXT        NOT NULL DEFAULT '',
    service_name    TEXT        NOT NULL DEFAULT '',
    config_path     TEXT        NOT NULL DEFAULT '',
    access_log_path TEXT        NOT NULL DEFAULT '',
    error_log_path  TEXT        NOT NULL DEFAULT '',
    vhosts          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    capabilities    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    observed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, node_id, kind, service_name, config_path)
);

CREATE INDEX IF NOT EXISTS idx_webserver_instances_tenant_node
    ON webserver_instances (tenant_id, node_id, kind);

CREATE TABLE IF NOT EXISTS webserver_config_actions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id               UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    webserver_instance_id UUID        REFERENCES webserver_instances(id) ON DELETE SET NULL,
    job_id                UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    action                TEXT        NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'pending'
                                          CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
    policy                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    result                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error_message         TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webserver_config_actions_node_status
    ON webserver_config_actions (node_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_webserver_config_actions_job
    ON webserver_config_actions (job_id);

CREATE TABLE IF NOT EXISTS webserver_config_receipts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id               UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    webserver_instance_id UUID        REFERENCES webserver_instances(id) ON DELETE SET NULL,
    action_id             UUID        REFERENCES webserver_config_actions(id) ON DELETE SET NULL,
    action                TEXT        NOT NULL,
    checksum_before       TEXT        NOT NULL DEFAULT '',
    checksum_after        TEXT        NOT NULL DEFAULT '',
    validation_status     TEXT        NOT NULL DEFAULT '',
    reload_status         TEXT        NOT NULL DEFAULT '',
    rollback_ref          TEXT        NOT NULL DEFAULT '',
    diff                  TEXT        NOT NULL DEFAULT '',
    metadata              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webserver_config_receipts_tenant_node
    ON webserver_config_receipts (tenant_id, node_id, created_at DESC);
