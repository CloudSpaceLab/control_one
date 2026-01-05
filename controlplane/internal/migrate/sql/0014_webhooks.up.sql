-- Webhooks for event notifications
CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    events TEXT[] NOT NULL DEFAULT '{}',
    secret TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    verify_ssl BOOLEAN NOT NULL DEFAULT true,
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    retry_count INTEGER NOT NULL DEFAULT 3,
    headers JSONB DEFAULT '{}',
    metadata JSONB DEFAULT '{}',
    last_triggered_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_webhooks_tenant_id ON webhooks(tenant_id);
CREATE INDEX idx_webhooks_enabled ON webhooks(enabled);
CREATE INDEX idx_webhooks_events ON webhooks USING GIN(events);

-- Webhook delivery history
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    event_id TEXT,
    status VARCHAR(50) NOT NULL, -- 'pending', 'success', 'failed', 'retrying'
    http_status_code INTEGER,
    request_body JSONB,
    response_body TEXT,
    error_message TEXT,
    attempt_number INTEGER NOT NULL DEFAULT 1,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_deliveries_webhook_id ON webhook_deliveries(webhook_id);
CREATE INDEX idx_webhook_deliveries_status ON webhook_deliveries(status);
CREATE INDEX idx_webhook_deliveries_created_at ON webhook_deliveries(created_at);
CREATE INDEX idx_webhook_deliveries_event_type ON webhook_deliveries(event_type);

COMMENT ON TABLE webhooks IS 'Webhook configurations for event notifications';
COMMENT ON COLUMN webhooks.tenant_id IS 'Tenant ID (NULL for global webhook)';
COMMENT ON COLUMN webhooks.events IS 'Array of event types to subscribe to';
COMMENT ON COLUMN webhooks.secret IS 'Secret for HMAC signature verification';
COMMENT ON COLUMN webhooks.verify_ssl IS 'Whether to verify SSL certificates';
COMMENT ON COLUMN webhooks.timeout_seconds IS 'HTTP request timeout in seconds';
COMMENT ON COLUMN webhooks.retry_count IS 'Number of retry attempts on failure';
COMMENT ON COLUMN webhooks.headers IS 'Additional HTTP headers to include';
COMMENT ON TABLE webhook_deliveries IS 'History of webhook delivery attempts';

