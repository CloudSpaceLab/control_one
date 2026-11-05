CREATE TABLE patch_organization_policy (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 revision bigint NOT NULL CHECK(revision > 0),
 status text NOT NULL CHECK(status IN ('draft','review','approved','withdrawn')),
 document jsonb NOT NULL,
 urgency_revision bigint NOT NULL,
 operations_revision bigint NOT NULL,
 authored_by uuid NOT NULL REFERENCES users(id),
 changed_by uuid NOT NULL REFERENCES users(id),
 approved_by uuid REFERENCES users(id),
 approved_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE TABLE patch_organization_history (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 revision bigint NOT NULL,
 status text NOT NULL,
 document jsonb NOT NULL,
 urgency_revision bigint NOT NULL,
 operations_revision bigint NOT NULL,
 authored_by uuid NOT NULL REFERENCES users(id),
 changed_by uuid NOT NULL REFERENCES users(id),
 approved_by uuid REFERENCES users(id),
 approved_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY(tenant_id,revision)
);
