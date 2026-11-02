-- Notification rows and audit/retention receipts are evidence. Routine binary
-- rollback should retain these additive structures. Explicit data migration is
-- required before reversing the widened notification contract.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM notifications WHERE kind='patch_review_escalation') THEN
  RAISE EXCEPTION 'Archive and migrate patch-review notification evidence before reversing migration 0177';
 END IF;
END $$;
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications DROP CONSTRAINT notifications_case_required;
ALTER TABLE notifications ALTER COLUMN case_id SET NOT NULL;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK(kind IN ('case_assigned','case_mentioned'));
DROP TABLE patch_review_operations_state;
DROP TABLE patch_review_escalation_receipts;
DROP TABLE patch_review_archives;
DROP TABLE patch_review_operations;
