ALTER TABLE squid_proxy_management ADD COLUMN retired_at timestamptz;
ALTER TABLE squid_proxy_management DROP CONSTRAINT squid_proxy_management_node_id_key;
CREATE UNIQUE INDEX squid_proxy_management_active_node ON squid_proxy_management(node_id) WHERE retired_at IS NULL;
