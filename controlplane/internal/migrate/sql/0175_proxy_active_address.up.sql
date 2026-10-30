ALTER TABLE squid_proxies DROP CONSTRAINT squid_proxies_tenant_id_host_port_key;
CREATE UNIQUE INDEX squid_proxies_active_address ON squid_proxies(tenant_id,host,port) WHERE status <> 'removed';
