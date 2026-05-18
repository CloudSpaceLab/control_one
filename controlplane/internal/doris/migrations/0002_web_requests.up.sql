CREATE TABLE IF NOT EXISTS web_requests (
  event_date       DATE          NOT NULL,
  tenant_id        VARCHAR(36)   NOT NULL,
  ts               DATETIME(3)   NOT NULL,
  node_id          VARCHAR(36),
  correlation_id   VARCHAR(36),
  webserver_kind   VARCHAR(32),
  server_group     VARCHAR(128),
  app              VARCHAR(128),
  vhost            VARCHAR(255),
  src_ip           VARCHAR(45),
  socket_ip        VARCHAR(45),
  xff_chain        STRING,
  country_code     VARCHAR(8),
  country          VARCHAR(128),
  asn              VARCHAR(64),
  isp              VARCHAR(255),
  reputation_score SMALLINT,
  method           VARCHAR(16),
  path_template    VARCHAR(255),
  path_hash        VARCHAR(64),
  status_code      INT,
  status_family    VARCHAR(3),
  bytes_out        BIGINT,
  bytes_in         BIGINT,
  duration_ms      BIGINT,
  upstream_status  VARCHAR(32),
  user_agent_hash  VARCHAR(64),
  referrer_host    VARCHAR(255),
  source_file      VARCHAR(512),
  parser_profile   VARCHAR(64),
  message          STRING,
  details_json     STRING,
  INDEX idx_web_msg (message) USING INVERTED PROPERTIES("parser"="english")
)
DUPLICATE KEY (event_date, tenant_id, ts)
PARTITION BY RANGE (event_date) ()
DISTRIBUTED BY HASH (tenant_id) BUCKETS 16
PROPERTIES (
  "replication_num"             = "1",
  "dynamic_partition.enable"    = "true",
  "dynamic_partition.time_unit" = "DAY",
  "dynamic_partition.start"     = "-90",
  "dynamic_partition.end"       = "3",
  "dynamic_partition.prefix"    = "p",
  "dynamic_partition.buckets"   = "16"
);
