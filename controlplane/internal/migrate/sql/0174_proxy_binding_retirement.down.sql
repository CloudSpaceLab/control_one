-- A host may have multiple retained retired bindings. Refuse rollback if
-- restoring the old uniqueness rule would discard their history.
DROP INDEX squid_proxy_management_active_node;
ALTER TABLE squid_proxy_management ADD CONSTRAINT squid_proxy_management_node_id_key UNIQUE(node_id);
ALTER TABLE squid_proxy_management DROP COLUMN retired_at;
