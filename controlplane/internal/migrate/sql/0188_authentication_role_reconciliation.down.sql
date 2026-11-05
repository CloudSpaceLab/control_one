DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_sources WHERE source='authentication') OR EXISTS(SELECT 1 FROM authentication_role_events) THEN
 RAISE EXCEPTION 'authentication grant evidence exists; restore a matching backup instead of destructive rollback';
 END IF;
END $$;
DROP TABLE authentication_role_events;
ALTER TABLE global_role_sources DROP CONSTRAINT global_role_sources_source_check;
ALTER TABLE global_role_sources ADD CONSTRAINT global_role_sources_source_check
 CHECK(source IN ('administrator','directory','legacy_unknown','provisioning'));
