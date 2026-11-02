DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM patch_automation_runs) THEN
  RAISE EXCEPTION 'Retained automation authorizations must be archived before migration rollback';
 END IF;
END $$;
DROP TABLE patch_automation_runs;
