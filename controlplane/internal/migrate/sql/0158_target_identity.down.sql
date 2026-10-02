DELETE FROM permissions WHERE name IN ('targets.read', 'targets.write');
DROP TRIGGER nodes_target_retire ON nodes;
DROP TRIGGER nodes_target_sync ON nodes;
DROP FUNCTION sync_node_target_trigger();
DROP FUNCTION sync_compute_target(nodes);
DROP TABLE target_addresses;
DROP TABLE targets;
ALTER TABLE nodes DROP CONSTRAINT nodes_id_tenant_unique;
