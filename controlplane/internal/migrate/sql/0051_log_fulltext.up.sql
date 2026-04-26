-- Enable trigram indexes for fast substring/regex search over log messages.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_telemetry_logs_message_trgm
    ON telemetry_logs USING GIN (log_message gin_trgm_ops);
