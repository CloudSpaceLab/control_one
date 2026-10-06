CREATE TABLE network_telemetry_sources (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL,
 target_id UUID NOT NULL,
 source_type TEXT NOT NULL CHECK (source_type IN ('snmp_poll','syslog')),
 collector_id TEXT NOT NULL,
 site TEXT NOT NULL DEFAULT '',
 sender_address TEXT NOT NULL DEFAULT '',
 stale_after_seconds INTEGER NOT NULL CHECK (stale_after_seconds BETWEEN 60 AND 86400),
 state TEXT NOT NULL DEFAULT 'not_configured' CHECK (state IN ('not_configured','ready','partial','stale','auth_failed','unreachable','unsupported','policy_blocked')),
 observed_at TIMESTAMPTZ,
 last_contact_at TIMESTAMPTZ,
 queue_depth BIGINT NOT NULL DEFAULT 0 CHECK (queue_depth >= 0),
 lag_millis BIGINT NOT NULL DEFAULT 0 CHECK (lag_millis >= 0),
 FOREIGN KEY (tenant_id,target_id) REFERENCES targets(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,collector_id) REFERENCES content_pack_edge_collectors(tenant_id,collector_id) ON DELETE CASCADE,
 UNIQUE (target_id,source_type)
);
-- A transport sender must resolve to exactly one target on the assigned collector.
CREATE UNIQUE INDEX network_syslog_sender ON network_telemetry_sources(tenant_id,collector_id,sender_address) WHERE source_type='syslog';
