-- Indexes that accelerate the SIEM Investigate "lifecycle" merge queries.
-- We only index columns that already exist in the Postgres schema; the
-- richer event store (src_ip / dst_ip / process_name / process_hash /
-- file_path / file_hash) lives in Doris.
--
-- Skipped columns (none of these exist on a Postgres table today, so no
-- index can be created against them — they live in Doris):
--   * security_events.src_ip / dst_ip
--   * security_events.process_name / process_hash
--   * security_events.file_path / file_hash
--   * security_events.user_id  (column not present)
--   * security_events.host_id  (security_events uses node_id; already indexed)
--
-- TODO(investigate): when Doris becomes the lifecycle source, add equivalent
-- bucket / bloom-filter indexes on the Doris `events` table.

-- Generic JSONB GIN index on security_events.details so substring searches
-- like `details ? 'src_ip'` and `details->>'src_ip' = $1` can use the index.
CREATE INDEX IF NOT EXISTS idx_security_events_details_gin
    ON security_events USING GIN (details);

-- node_id is already indexed but the lifecycle query joins on
-- (tenant_id, node_id, fired_at) - add a covering composite.
CREATE INDEX IF NOT EXISTS idx_security_events_tenant_node_fired
    ON security_events (tenant_id, node_id, fired_at DESC)
    WHERE node_id IS NOT NULL;

-- alerts.context is JSONB; same pattern.
CREATE INDEX IF NOT EXISTS idx_alerts_context_gin
    ON alerts USING GIN (context);
CREATE INDEX IF NOT EXISTS idx_alerts_tenant_node_opened
    ON alerts (tenant_id, node_id, opened_at DESC)
    WHERE node_id IS NOT NULL;

-- audit_logs.metadata is JSONB; resource_type + resource_id already indexed.
CREATE INDEX IF NOT EXISTS idx_audit_logs_metadata_gin
    ON audit_logs USING GIN (metadata);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_created
    ON audit_logs (tenant_id, created_at DESC);

-- session_recordings: investigate by user_id (text) or node_id is common.
CREATE INDEX IF NOT EXISTS idx_session_recordings_node_started
    ON session_recordings (node_id, started_at DESC);
