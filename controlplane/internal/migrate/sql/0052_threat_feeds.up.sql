CREATE TABLE IF NOT EXISTS threat_feeds (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    feed_type           TEXT NOT NULL CHECK (feed_type IN (
        'spamhaus_drop','spamhaus_edrop','firehol_l1','tor_exit',
        'abuseipdb','otx','custom_lines','custom_spamhaus'
    )),
    url                 TEXT,
    api_key_sealed      BYTEA,
    nonce               BYTEA,
    score_floor         INTEGER NOT NULL DEFAULT 50 CHECK (score_floor BETWEEN 0 AND 100),
    refresh_seconds     INTEGER NOT NULL DEFAULT 3600 CHECK (refresh_seconds >= 60),
    category            TEXT,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    last_status         TEXT,
    last_error          TEXT,
    last_indicator_count INTEGER NOT NULL DEFAULT 0,
    last_refreshed_at   TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_threat_feeds_tenant   ON threat_feeds (tenant_id);
CREATE INDEX IF NOT EXISTS idx_threat_feeds_enabled  ON threat_feeds (enabled);
CREATE INDEX IF NOT EXISTS idx_threat_feeds_type     ON threat_feeds (feed_type);
