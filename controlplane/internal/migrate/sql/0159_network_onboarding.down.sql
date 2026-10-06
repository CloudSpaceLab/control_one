DELETE FROM role_permissions WHERE permission_name='targets.connect';
DELETE FROM permissions WHERE name='targets.connect';
DROP TABLE network_target_connections;
DROP TABLE network_connection_tests;
ALTER TABLE provider_credentials DROP CONSTRAINT provider_credentials_id_tenant_unique;
