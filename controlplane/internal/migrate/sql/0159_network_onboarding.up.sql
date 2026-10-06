-- Reuse the encrypted credential store; device rows contain references only.
ALTER TABLE provider_credentials ADD CONSTRAINT provider_credentials_id_tenant_unique UNIQUE(id, tenant_id);
CREATE TABLE network_connection_tests (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 user_id UUID NOT NULL REFERENCES users(id),
 credential_id UUID NOT NULL,
 protocol TEXT NOT NULL CHECK(protocol IN ('snmpv3','ssh')),
 address TEXT NOT NULL,
 port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
 state TEXT NOT NULL CHECK(state IN ('testing','authenticated','auth_failed','unreachable','unsupported','policy_blocked')),
 result JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 completed_at TIMESTAMPTZ,
 target_id UUID,
 FOREIGN KEY(credential_id,tenant_id) REFERENCES provider_credentials(id,tenant_id),
 FOREIGN KEY(target_id,tenant_id) REFERENCES targets(id,tenant_id),
 UNIQUE(id,tenant_id)
);
CREATE INDEX network_connection_tests_tenant_idx ON network_connection_tests(tenant_id,created_at DESC);
CREATE TABLE network_target_connections (
 target_id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL,
 credential_id UUID NOT NULL,
 test_id UUID NOT NULL,
 protocol TEXT NOT NULL,
 address TEXT NOT NULL,
 port INTEGER NOT NULL,
 telemetry_sources JSONB NOT NULL DEFAULT '[]',
 FOREIGN KEY(target_id,tenant_id) REFERENCES targets(id,tenant_id) ON DELETE CASCADE,
 FOREIGN KEY(credential_id,tenant_id) REFERENCES provider_credentials(id,tenant_id),
 FOREIGN KEY(test_id,tenant_id) REFERENCES network_connection_tests(id,tenant_id)
);
INSERT INTO permissions(name,description,category) VALUES
 ('targets.connect','Test read-only network connections using encrypted secret references','inventory');
INSERT INTO role_permissions(role_id,permission_name)
 SELECT id,'targets.connect' FROM roles WHERE name IN ('admin','operator');
