-- Unified event pipeline: events, process_connections, process_lineage,
-- file_accesses, db_queries plus the dashboard rollup MV.
--
-- Conventions:
--   * Daily partitioning by event_date for cheap TTL via partition drop.
--   * BUCKETS sized so worst-case node × hour stays in memory cleanly.
--   * dynamic_partition.start picks per-table retention; raise via ALTER if
--     the customer needs a longer evidence window.

-- Doris requires DUPLICATE KEY columns to be an ordered prefix of the
-- schema. Every table below puts (event_date, tenant_id, <time>, ...)
-- first; node_id and the rest of the payload follow. Re-ordering after
-- the original draft because Doris errored with
-- `KeyColumns[2] (...) is ts, but corresponding column is node_id`.

CREATE TABLE IF NOT EXISTS events (
  event_date         DATE          NOT NULL,
  tenant_id          VARCHAR(36)   NOT NULL,
  ts                 DATETIME(3)   NOT NULL,
  node_id            VARCHAR(36),
  event_type         VARCHAR(32)   NOT NULL,
  severity           VARCHAR(16),
  correlation_id     VARCHAR(36),
  conn_id            VARCHAR(36),
  bastion_session_id VARCHAR(36),
  pid                BIGINT,
  process_name       VARCHAR(128),
  user_name          VARCHAR(64),
  src_ip             VARCHAR(45),
  src_port           INT,
  dst_ip             VARCHAR(45),
  dst_port           INT,
  protocol           VARCHAR(8),
  bytes_in           BIGINT,
  bytes_out          BIGINT,
  duration_ms        BIGINT,
  rule_id            VARCHAR(36),
  threat_feed        VARCHAR(64),
  threat_score       SMALLINT,
  message            STRING,
  details_json       STRING,
  dedup_key          VARCHAR(128),
  INDEX idx_msg (message) USING INVERTED PROPERTIES("parser"="english")
)
DUPLICATE KEY (event_date, tenant_id, ts)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 16
PROPERTIES (
  "replication_num"            = "1",
  "dynamic_partition.enable"   = "true",
  "dynamic_partition.time_unit"= "DAY",
  "dynamic_partition.start"    = "-90",
  "dynamic_partition.end"      = "3",
  "dynamic_partition.prefix"   = "p",
  "dynamic_partition.buckets"  = "16"
);

CREATE TABLE IF NOT EXISTS process_connections (
  event_date         DATE          NOT NULL,
  tenant_id          VARCHAR(36)   NOT NULL,
  started_at         DATETIME(3)   NOT NULL,
  conn_id            VARCHAR(36)   NOT NULL,
  node_id            VARCHAR(36),
  correlation_id     VARCHAR(36),
  bastion_session_id VARCHAR(36),
  ended_at           DATETIME(3),
  last_data_at       DATETIME(3),
  duration_ms        BIGINT,
  direction          VARCHAR(16),
  pid                BIGINT,
  process_name       VARCHAR(128),
  cmdline            VARCHAR(512),
  user_name          VARCHAR(64),
  uid                INT,
  gid                INT,
  exe_hash           VARCHAR(32),
  src_ip             VARCHAR(45),
  src_port           INT,
  dst_ip             VARCHAR(45),
  dst_port           INT,
  protocol           VARCHAR(8),
  bytes_in           BIGINT,
  bytes_out          BIGINT,
  packets_in         BIGINT,
  packets_out        BIGINT,
  threat_match       BOOLEAN,
  threat_feed        VARCHAR(64),
  threat_score       SMALLINT,
  closed_reason      VARCHAR(32)
)
DUPLICATE KEY (event_date, tenant_id, started_at, conn_id)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 8
PROPERTIES (
  "replication_num"            = "1",
  "dynamic_partition.enable"   = "true",
  "dynamic_partition.time_unit"= "DAY",
  "dynamic_partition.start"    = "-60",
  "dynamic_partition.end"      = "3",
  "dynamic_partition.prefix"   = "p",
  "dynamic_partition.buckets"  = "8"
);

CREATE TABLE IF NOT EXISTS process_lineage (
  event_date     DATE          NOT NULL,
  tenant_id      VARCHAR(36)   NOT NULL,
  observed_at    DATETIME(3)   NOT NULL,
  pid            BIGINT,
  node_id        VARCHAR(36),
  ppid           BIGINT,
  process_name   VARCHAR(128),
  cmdline        VARCHAR(2048),
  user_name      VARCHAR(64),
  uid            INT,
  gid            INT,
  exe_path       VARCHAR(512),
  exe_hash       VARCHAR(32),
  exited_at      DATETIME(3)
)
DUPLICATE KEY (event_date, tenant_id, observed_at, pid)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 4
PROPERTIES (
  "replication_num"            = "1",
  "dynamic_partition.enable"   = "true",
  "dynamic_partition.time_unit"= "DAY",
  "dynamic_partition.start"    = "-30",
  "dynamic_partition.end"      = "3",
  "dynamic_partition.prefix"   = "p",
  "dynamic_partition.buckets"  = "4"
);

CREATE TABLE IF NOT EXISTS file_accesses (
  event_date     DATE          NOT NULL,
  tenant_id      VARCHAR(36)   NOT NULL,
  ts             DATETIME(3)   NOT NULL,
  pid            BIGINT,
  node_id        VARCHAR(36),
  correlation_id VARCHAR(36),
  conn_id        VARCHAR(36),
  process_name   VARCHAR(128),
  user_name      VARCHAR(64),
  path           VARCHAR(1024),
  op             VARCHAR(16),
  bytes          BIGINT,
  op_count       INT,
  started_at     DATETIME(3),
  ended_at       DATETIME(3)
)
DUPLICATE KEY (event_date, tenant_id, ts, pid)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 8
PROPERTIES (
  "replication_num"            = "1",
  "dynamic_partition.enable"   = "true",
  "dynamic_partition.time_unit"= "DAY",
  "dynamic_partition.start"    = "-90",
  "dynamic_partition.end"      = "3",
  "dynamic_partition.prefix"   = "p",
  "dynamic_partition.buckets"  = "8"
);

CREATE TABLE IF NOT EXISTS db_queries (
  event_date     DATE          NOT NULL,
  tenant_id      VARCHAR(36)   NOT NULL,
  ts             DATETIME(3)   NOT NULL,
  pid            BIGINT,
  node_id        VARCHAR(36),
  correlation_id VARCHAR(36),
  conn_id        VARCHAR(36),
  engine         VARCHAR(16),
  database_name  VARCHAR(128),
  user_name      VARCHAR(64),
  src_ip         VARCHAR(45),
  query_hash     VARCHAR(32),
  query_text     VARCHAR(512),
  rows_affected  BIGINT,
  exec_time_ms   BIGINT,
  started_at     DATETIME(3),
  ended_at       DATETIME(3),
  tables_touched VARCHAR(512)
)
DUPLICATE KEY (event_date, tenant_id, ts, pid)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 8
PROPERTIES (
  "replication_num"            = "1",
  "dynamic_partition.enable"   = "true",
  "dynamic_partition.time_unit"= "DAY",
  "dynamic_partition.start"    = "-90",
  "dynamic_partition.end"      = "3",
  "dynamic_partition.prefix"   = "p",
  "dynamic_partition.buckets"  = "8"
);

CREATE MATERIALIZED VIEW IF NOT EXISTS events_per_hour_mv
DISTRIBUTED BY HASH (tenant_id) BUCKETS 4
PROPERTIES (
  "replication_num" = "1"
)
AS
  SELECT tenant_id,
         node_id,
         event_type,
         date_trunc(ts, 'hour') AS hour_ts,
         COUNT(*)              AS cnt,
         MAX(severity)         AS sev_max,
         SUM(bytes_in)         AS bytes_in_sum,
         SUM(bytes_out)        AS bytes_out_sum
  FROM events
  GROUP BY tenant_id, node_id, event_type, hour_ts;
