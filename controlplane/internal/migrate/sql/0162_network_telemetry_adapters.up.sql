ALTER TABLE network_telemetry_sources DROP CONSTRAINT network_telemetry_sources_source_type_check;
ALTER TABLE network_telemetry_sources ADD CONSTRAINT network_telemetry_sources_source_type_check CHECK (source_type IN ('snmp_poll','snmp_trap','syslog','netflow','ipfix','sflow','ssh_config','netconf','restconf','vendor_api'));
CREATE UNIQUE INDEX network_receiver_sender ON network_telemetry_sources(tenant_id,collector_id,source_type,sender_address) WHERE source_type IN ('snmp_trap','netflow','ipfix','sflow');
