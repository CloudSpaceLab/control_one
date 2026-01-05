DROP INDEX IF EXISTS telemetry_logs_tenant_timestamp_idx;
DROP INDEX IF EXISTS telemetry_logs_level_idx;
DROP INDEX IF EXISTS telemetry_logs_timestamp_idx;
DROP INDEX IF EXISTS telemetry_logs_node_id_idx;
DROP INDEX IF EXISTS telemetry_logs_tenant_id_idx;

DROP INDEX IF EXISTS telemetry_metrics_tenant_timestamp_idx;
DROP INDEX IF EXISTS telemetry_metrics_name_idx;
DROP INDEX IF EXISTS telemetry_metrics_timestamp_idx;
DROP INDEX IF EXISTS telemetry_metrics_node_id_idx;
DROP INDEX IF EXISTS telemetry_metrics_tenant_id_idx;

DROP TABLE IF EXISTS telemetry_logs;
DROP TABLE IF EXISTS telemetry_metrics;



