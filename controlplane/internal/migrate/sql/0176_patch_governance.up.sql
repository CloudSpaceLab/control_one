CREATE TABLE patch_governance_policy (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 revision bigint NOT NULL CHECK(revision>0),
 document jsonb NOT NULL,
 updated_by text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE TABLE patch_governance_tracking (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 finding_id uuid NOT NULL REFERENCES node_vulnerability_findings(id),
 revision bigint NOT NULL CHECK(revision>0),
 document jsonb NOT NULL,
 updated_by text NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY(tenant_id,finding_id)
);
CREATE TABLE patch_governance_events (
 id bigserial PRIMARY KEY,
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 finding_id uuid REFERENCES node_vulnerability_findings(id),
 kind text NOT NULL,
 revision bigint NOT NULL,
 actor text NOT NULL,
 document jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX patch_governance_events_tenant ON patch_governance_events(tenant_id,id);
