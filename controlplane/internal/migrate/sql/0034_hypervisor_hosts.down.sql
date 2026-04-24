DROP INDEX IF EXISTS idx_clusters_hypervisor_host;
ALTER TABLE clusters DROP COLUMN IF EXISTS hypervisor_host_id;
DROP INDEX IF EXISTS idx_hypervisor_hosts_credential;
DROP INDEX IF EXISTS idx_hypervisor_hosts_provider;
DROP INDEX IF EXISTS idx_hypervisor_hosts_tenant;
DROP TABLE IF EXISTS hypervisor_hosts;
