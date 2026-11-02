CREATE TABLE patch_automation_runs (
 id uuid PRIMARY KEY,
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 approval_id uuid NOT NULL UNIQUE REFERENCES patch_approvals(id),
 policy_revision bigint NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 authority text NOT NULL CHECK(authority IN ('individual_approval','tenant_policy')),
 policy_hash text NOT NULL,
 not_before timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'waiting' CHECK(status IN ('waiting','blocked','dispatched','cancelled','expired','failed')),
 reason text NOT NULL DEFAULT '',
 execution_job_id uuid REFERENCES jobs(id),
 created_at timestamptz NOT NULL DEFAULT NOW(),
 checked_at timestamptz,
 CHECK(expires_at>not_before)
);
CREATE INDEX patch_automation_runs_due ON patch_automation_runs(not_before,id) WHERE status IN ('waiting','blocked');
CREATE INDEX patch_automation_runs_tenant ON patch_automation_runs(tenant_id,created_at,id);
