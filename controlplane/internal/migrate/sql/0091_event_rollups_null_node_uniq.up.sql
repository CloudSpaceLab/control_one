-- The original PK on event_rollups_hourly is (tenant_id, node_id,
-- event_type, hour_ts), but Postgres treats NULL node_id as DISTINCT in
-- composite unique constraints. The IncrementHourlyRollup upsert that
-- inserts (tenant, NULL, type, hour) therefore could never match an
-- existing row via ON CONFLICT, leaving duplicate accumulating rows for
-- every node-less event.
--
-- A partial unique index covers the NULL case so the upsert in the Go
-- code (`ON CONFLICT ... DO UPDATE`) can dedupe correctly.
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_rollups_hourly_null_node
    ON event_rollups_hourly (tenant_id, event_type, hour_ts)
    WHERE node_id IS NULL;
