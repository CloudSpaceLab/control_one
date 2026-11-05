DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM patch_organization_history) THEN
  RAISE EXCEPTION 'retained organizational sign-off history prevents destructive rollback';
 END IF;
END $$;
DROP TABLE patch_organization_history;
DROP TABLE patch_organization_policy;
