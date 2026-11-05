DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_source_reviews) THEN
  RAISE EXCEPTION 'role-source review evidence exists; retain it before rollback';
 END IF;
END $$;
DROP TABLE global_role_source_reviews;
