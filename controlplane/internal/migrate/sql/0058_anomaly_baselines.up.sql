-- Behavioral anomaly baselines + first-seen registries. Powers Phase F
-- detectors covering the five highest-signal attack vectors:
--   1. New external destination IP                       → tenant_known_destinations
--   2. Existing IP stays longer than its rolling p95     → connection_duration_baselines
--   3. Never-before-seen executable hash on this tenant  → tenant_known_exe_hashes
--   4. Bytes/packets exceed normal for this process+port → connection_bytes_baselines
--   5. New / high-rows DB query                          → db_query_known_hashes
--
-- All tables are intentionally Postgres-resident so the ingest path can
-- do a single index-lookup per event without crossing the Doris boundary.
-- The hourly worker job recomputes percentiles from Doris and writes back.

-- Attack #1: every external destination an agent has seen, per tenant.
CREATE TABLE IF NOT EXISTS tenant_known_destinations (
    tenant_id     UUID NOT NULL,
    dst_ip        INET NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    conn_count    BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, dst_ip)
);
CREATE INDEX IF NOT EXISTS idx_tenant_known_dst_first_seen
    ON tenant_known_destinations (tenant_id, first_seen_at DESC);

-- Attack #2: per (tenant, dst_ip, dst_port) duration percentiles refreshed
-- every hour from `events` (event_type='conn.close') in Doris.
CREATE TABLE IF NOT EXISTS connection_duration_baselines (
    tenant_id    UUID NOT NULL,
    dst_ip       INET NOT NULL,
    dst_port     INTEGER NOT NULL,
    p50_ms       BIGINT,
    p95_ms       BIGINT,
    p99_ms       BIGINT,
    sample_count BIGINT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, dst_ip, dst_port)
);

-- Attack #3: every executable hash seen on a tenant. Insert ⇒ first sighting
-- = anomaly.new_executable. last_seen_at + exec_count fed by every
-- subsequent proc.exec.
CREATE TABLE IF NOT EXISTS tenant_known_exe_hashes (
    tenant_id        UUID NOT NULL,
    exe_hash         VARCHAR(64) NOT NULL,
    first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    first_seen_pid   BIGINT,
    first_seen_path  TEXT,
    first_seen_node  UUID,
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    exec_count       BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, exe_hash)
);
CREATE INDEX IF NOT EXISTS idx_known_exe_first_seen
    ON tenant_known_exe_hashes (tenant_id, first_seen_at DESC);

-- Attack #4: per (tenant, process_name, dst_port) byte/packet percentiles
-- refreshed hourly. Used at conn.close ingest to flag exfil signals.
CREATE TABLE IF NOT EXISTS connection_bytes_baselines (
    tenant_id        UUID NOT NULL,
    process_name     TEXT NOT NULL,
    dst_port         INTEGER NOT NULL,
    p95_bytes_in     BIGINT,
    p95_bytes_out    BIGINT,
    p95_packets_in   BIGINT,
    p95_packets_out  BIGINT,
    sample_count     BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, process_name, dst_port)
);

-- Attack #5: every (engine, db, user, query_hash) tuple a tenant has
-- executed. Insert ⇒ anomaly.new_db_query. avg/p95/max columns fed by
-- the hourly rollup so we can also flag rows-affected anomalies.
CREATE TABLE IF NOT EXISTS db_query_known_hashes (
    tenant_id     UUID NOT NULL,
    engine        VARCHAR(16) NOT NULL,
    database_name VARCHAR(128) NOT NULL,
    user_name     VARCHAR(64) NOT NULL,
    query_hash    VARCHAR(32) NOT NULL,
    query_sample  TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    exec_count    BIGINT NOT NULL DEFAULT 1,
    avg_rows      BIGINT,
    p95_rows      BIGINT,
    max_rows      BIGINT,
    avg_exec_ms   BIGINT,
    p95_exec_ms   BIGINT,
    max_exec_ms   BIGINT,
    PRIMARY KEY (tenant_id, engine, database_name, user_name, query_hash)
);
CREATE INDEX IF NOT EXISTS idx_known_query_first_seen
    ON db_query_known_hashes (tenant_id, first_seen_at DESC);
