DROP INDEX IF EXISTS idx_siem_forwarding_attempts_destination_time;
DROP TABLE IF EXISTS siem_forwarding_delivery_attempts;

DROP INDEX IF EXISTS idx_siem_forwarding_checkpoints_tenant_cursor;
DROP TABLE IF EXISTS siem_forwarding_checkpoints;

DROP INDEX IF EXISTS idx_siem_forwarding_destinations_tenant_status;
DROP TABLE IF EXISTS siem_forwarding_destinations;
