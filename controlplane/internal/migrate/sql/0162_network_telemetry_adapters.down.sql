DELETE FROM network_telemetry_sources WHERE source_type NOT IN ('snmp_poll','syslog');
DROP INDEX network_receiver_sender;
ALTER TABLE network_telemetry_sources DROP CONSTRAINT network_telemetry_sources_source_type_check;
ALTER TABLE network_telemetry_sources ADD CONSTRAINT network_telemetry_sources_source_type_check CHECK (source_type IN ('snmp_poll','syslog'));
