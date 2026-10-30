CREATE TABLE squid_proxy_management (
    proxy_id uuid PRIMARY KEY REFERENCES squid_proxies(id),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    node_id uuid NOT NULL UNIQUE REFERENCES nodes(id),
    revision bigint NOT NULL CHECK (revision > 0),
    client_cidrs jsonb NOT NULL DEFAULT '[]'::jsonb,
    probe_urls jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_job_id uuid REFERENCES jobs(id),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX squid_proxy_management_tenant ON squid_proxy_management(tenant_id);
