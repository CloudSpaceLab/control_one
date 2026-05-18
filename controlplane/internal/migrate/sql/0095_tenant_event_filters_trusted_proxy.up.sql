ALTER TABLE tenant_event_filters
    ADD COLUMN IF NOT EXISTS trusted_proxy_cidrs TEXT[] NOT NULL DEFAULT '{}';
