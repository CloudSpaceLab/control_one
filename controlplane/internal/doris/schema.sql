-- Doris analytic schema for Control One.
--
-- Design choices:
--   * DUPLICATE KEY tables for raw event streams — append-only, fast scans.
--   * AGGREGATE KEY for metrics — pre-rollups by minute/hour reduce read cost.
--   * Range partitioning by day so retention drops a partition instead of
--     deleting rows; combined with TTL keeps storage bounded.
--   * BITMAP columns for unique counts (DISTINCT user/IP/device) — fixed cost
--     regardless of cardinality, exact via roaring bitmaps.
--   * Replication factor 3 in production; lowered to 1 in single-node dev.

-- telemetry_logs ----------------------------------------------------------
CREATE TABLE IF NOT EXISTS telemetry_logs (
    `event_date`   DATE             NOT NULL,
    `tenant_id`    VARCHAR(36)      NOT NULL,
    `node_id`      VARCHAR(36),
    `timestamp`    DATETIME(3)      NOT NULL,
    `log_level`    VARCHAR(16),
    `log_source`   VARCHAR(64),
    `log_program`  VARCHAR(128),
    `message`      STRING,
    `labels_json`  STRING,
    INDEX idx_message (message) USING INVERTED PROPERTIES("parser"="english"),
    INDEX idx_source  (log_source) USING INVERTED
)
DUPLICATE KEY (`event_date`, `tenant_id`, `timestamp`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 8
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-90",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "8"
);

-- security_events ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS security_events (
    `event_date`  DATE          NOT NULL,
    `tenant_id`   VARCHAR(36)   NOT NULL,
    `node_id`     VARCHAR(36),
    `fired_at`    DATETIME(3)   NOT NULL,
    `event_type`  VARCHAR(64)   NOT NULL,
    `severity`    VARCHAR(16)   NOT NULL,
    `source`      VARCHAR(64)   NOT NULL,
    `dedup_key`   VARCHAR(128),
    `details`     STRING,
    `src_ip`      VARCHAR(45),
    `dst_ip`      VARCHAR(45)
)
DUPLICATE KEY (`event_date`, `tenant_id`, `fired_at`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 8
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-365",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "8"
);

-- rule_trigger_log --------------------------------------------------------
CREATE TABLE IF NOT EXISTS rule_trigger_log (
    `event_date`    DATE         NOT NULL,
    `tenant_id`     VARCHAR(36)  NOT NULL,
    `triggered_at`  DATETIME(3)  NOT NULL,
    `rule_id`       VARCHAR(36)  NOT NULL,
    `rule_type`     VARCHAR(16)  NOT NULL,
    `node_id`       VARCHAR(36),
    `severity`      VARCHAR(16),
    `details`       STRING
)
DUPLICATE KEY (`event_date`, `tenant_id`, `triggered_at`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 4
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-180",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "4"
);

-- telemetry_metrics (aggregate rollup) ------------------------------------
CREATE TABLE IF NOT EXISTS telemetry_metrics_1m (
    `event_date`   DATE          NOT NULL,
    `tenant_id`    VARCHAR(36)   NOT NULL,
    `node_id`      VARCHAR(36)   NOT NULL,
    `metric_name`  VARCHAR(128)  NOT NULL,
    `bucket_ts`    DATETIME      NOT NULL,
    `value_sum`    DOUBLE        SUM      DEFAULT "0",
    `value_count`  BIGINT        SUM      DEFAULT "0",
    `value_max`    DOUBLE        MAX      DEFAULT "0",
    `value_min`    DOUBLE        MIN      DEFAULT "0"
)
AGGREGATE KEY (`event_date`, `tenant_id`, `node_id`, `metric_name`, `bucket_ts`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 8
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-30",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "8"
);

-- unique_counters: BITMAP-backed exact-distinct rollup --------------------
-- Use case: "unique source IPs that hit rule X this hour" without keeping
-- raw rows. BITMAP_UNION on insert; BITMAP_UNION_COUNT on read.
CREATE TABLE IF NOT EXISTS unique_counters (
    `event_date`   DATE          NOT NULL,
    `tenant_id`    VARCHAR(36)   NOT NULL,
    `dimension`    VARCHAR(64)   NOT NULL,  -- e.g. "rule_id"
    `dim_value`    VARCHAR(255)  NOT NULL,
    `bucket_ts`    DATETIME      NOT NULL,
    `unique_set`   BITMAP        BITMAP_UNION
)
AGGREGATE KEY (`event_date`, `tenant_id`, `dimension`, `dim_value`, `bucket_ts`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 4
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-180",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "4"
);

-- threat_observations: bad-IP / abuse-source feed history -----------------
CREATE TABLE IF NOT EXISTS threat_observations (
    `event_date`  DATE          NOT NULL,
    `tenant_id`   VARCHAR(36)   NOT NULL,
    `observed_at` DATETIME(3)   NOT NULL,
    `ip`          VARCHAR(45)   NOT NULL,
    `feed`        VARCHAR(64)   NOT NULL,
    `category`    VARCHAR(64),
    `score`       SMALLINT      DEFAULT "0",
    `evidence`    STRING
)
DUPLICATE KEY (`event_date`, `tenant_id`, `observed_at`, `ip`)
PARTITION BY RANGE (`event_date`) ()
DISTRIBUTED BY HASH(`tenant_id`) BUCKETS 8
PROPERTIES (
    "replication_num"            = "1",
    "dynamic_partition.enable"   = "true",
    "dynamic_partition.time_unit"= "DAY",
    "dynamic_partition.start"    = "-90",
    "dynamic_partition.end"      = "3",
    "dynamic_partition.prefix"   = "p",
    "dynamic_partition.buckets"  = "8"
);
