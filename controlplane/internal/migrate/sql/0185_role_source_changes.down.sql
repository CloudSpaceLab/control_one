DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_source_reviews WHERE action <> 'claim_legacy') THEN
  RAISE EXCEPTION 'source-change evidence exists; retain it before rollback';
 END IF;
END $$;
ALTER TABLE global_role_source_reviews DROP COLUMN action, DROP COLUMN before_expiry, DROP COLUMN after_expiry;
