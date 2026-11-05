ALTER TABLE global_role_sources DROP CONSTRAINT global_role_sources_source_check;
ALTER TABLE global_role_sources ADD CONSTRAINT global_role_sources_source_check
 CHECK(source IN ('administrator','directory','legacy_unknown','provisioning'));
