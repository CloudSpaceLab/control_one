CREATE TABLE network_inventory (
 target_id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL,
 refresh_id UUID NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('refreshing','inventory_ready','auth_failed','unreachable','unsupported','policy_blocked')),
 attempted_at TIMESTAMPTZ NOT NULL,
 completed_at TIMESTAMPTZ,
 snapshot JSONB,
 FOREIGN KEY(target_id,tenant_id) REFERENCES targets(id,tenant_id) ON DELETE CASCADE
);
CREATE INDEX network_inventory_tenant_idx ON network_inventory(tenant_id);
