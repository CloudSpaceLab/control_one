-- A rollback discards source metadata. Do not silently destroy attribution.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM global_role_sources WHERE source <> 'legacy_unknown') THEN
        RAISE EXCEPTION 'global role source attribution exists; reconcile it before rollback';
    END IF;
END $$;
DROP TABLE global_role_sources;
