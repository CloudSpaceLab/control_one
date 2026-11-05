DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_sources WHERE source='provisioning') THEN
  RAISE EXCEPTION 'provisioning grants exist; review attribution before rollback';
 END IF;
END $$;
ALTER TABLE global_role_sources DROP CONSTRAINT global_role_sources_source_check;
ALTER TABLE global_role_sources ADD CONSTRAINT global_role_sources_source_check
 CHECK(source IN ('administrator','directory','legacy_unknown'));
