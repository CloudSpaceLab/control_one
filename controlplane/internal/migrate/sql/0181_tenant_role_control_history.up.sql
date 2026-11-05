CREATE TABLE tenant_role_controls (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id), revision bigint NOT NULL CHECK(revision>0),
 changed_by uuid NOT NULL REFERENCES users(id), updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE TABLE tenant_role_control_events (
 tenant_id uuid NOT NULL REFERENCES tenants(id), revision bigint NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id), user_id uuid NOT NULL REFERENCES users(id),
 assignment_id uuid NOT NULL REFERENCES user_roles(id), action text NOT NULL CHECK(action IN ('grant','revoke')),
 role text NOT NULL, expires_at timestamptz NOT NULL, reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(), PRIMARY KEY(tenant_id,revision)
);
