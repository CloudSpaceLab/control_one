-- Retained historical addresses may repeat; rollback refuses duplicates
-- instead of deleting audit history.
DROP INDEX squid_proxies_active_address;
ALTER TABLE squid_proxies ADD CONSTRAINT squid_proxies_tenant_id_host_port_key UNIQUE(tenant_id,host,port);
