CREATE TABLE IF NOT EXISTS telemetry_metrics (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    metric_name TEXT NOT NULL,
    metric_value DOUBLE PRECISION NOT NULL,
    metric_unit TEXT,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    timestamp TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS telemetry_logs (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    log_level TEXT NOT NULL,
    log_message TEXT NOT NULL,
    log_source TEXT,
    log_program TEXT,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    timestamp TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS telemetry_metrics_tenant_id_idx ON telemetry_metrics(tenant_id);
CREATE INDEX IF NOT EXISTS telemetry_metrics_node_id_idx ON telemetry_metrics(node_id);
CREATE INDEX IF NOT EXISTS telemetry_metrics_timestamp_idx ON telemetry_metrics(timestamp);
CREATE INDEX IF NOT EXISTS telemetry_metrics_name_idx ON telemetry_metrics(metric_name);
CREATE INDEX IF NOT EXISTS telemetry_metrics_tenant_timestamp_idx ON telemetry_metrics(tenant_id, timestamp);

CREATE INDEX IF NOT EXISTS telemetry_logs_tenant_id_idx ON telemetry_logs(tenant_id);
CREATE INDEX IF NOT EXISTS telemetry_logs_node_id_idx ON telemetry_logs(node_id);
CREATE INDEX IF NOT EXISTS telemetry_logs_timestamp_idx ON telemetry_logs(timestamp);
CREATE INDEX IF NOT EXISTS telemetry_logs_level_idx ON telemetry_logs(log_level);
CREATE INDEX IF NOT EXISTS telemetry_logs_tenant_timestamp_idx ON telemetry_logs(tenant_id, timestamp);



