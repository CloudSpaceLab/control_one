CREATE TABLE patch_review_operations (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id), revision bigint NOT NULL CHECK(revision>0),
 document jsonb NOT NULL, updated_by text NOT NULL, updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE TABLE patch_review_archives (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), actor text NOT NULL,
 sha256 text NOT NULL, high_event_id bigint NOT NULL, counts jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX patch_review_archives_tenant ON patch_review_archives(tenant_id,created_at);
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ALTER COLUMN case_id DROP NOT NULL;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK(kind IN ('case_assigned','case_mentioned','patch_review_escalation'));
ALTER TABLE notifications ADD CONSTRAINT notifications_case_required CHECK(kind='patch_review_escalation' OR case_id IS NOT NULL);
CREATE TABLE patch_review_escalation_receipts (
 tenant_id uuid NOT NULL REFERENCES tenants(id), finding_id uuid NOT NULL REFERENCES node_vulnerability_findings(id),
 recipient_id uuid NOT NULL REFERENCES users(id), review_revision bigint NOT NULL, operations_revision bigint NOT NULL,
 interval_bucket bigint NOT NULL, notification_id uuid REFERENCES notifications(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY(tenant_id,finding_id,recipient_id,review_revision,operations_revision,interval_bucket)
);
CREATE TABLE patch_review_operations_state (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id), checked_at timestamptz NOT NULL,
 delivered integer NOT NULL DEFAULT 0,last_error text NOT NULL DEFAULT ''
);
