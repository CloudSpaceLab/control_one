CREATE TABLE network_configuration_snapshots (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL,
 target_id UUID NOT NULL,
 source_id UUID NOT NULL,
 source_type TEXT NOT NULL CHECK (source_type IN ('ssh_config','netconf','restconf','vendor_api')),
 adapter TEXT NOT NULL,
 adapter_version TEXT NOT NULL,
 format TEXT NOT NULL CHECK (format IN ('text','json','xml')),
 content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 16384),
 content_hash TEXT NOT NULL CHECK (content_hash ~ '^[a-f0-9]{64}$'),
 revision INTEGER NOT NULL CHECK (revision > 0),
 observed_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY (target_id,tenant_id) REFERENCES targets(id,tenant_id) ON DELETE CASCADE,
 UNIQUE (target_id,source_type,revision),
 UNIQUE (target_id,source_type,content_hash)
);
CREATE INDEX network_configuration_snapshots_history_idx
 ON network_configuration_snapshots(tenant_id,target_id,source_type,revision DESC);
