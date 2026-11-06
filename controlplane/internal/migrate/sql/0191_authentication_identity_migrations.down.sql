DO $$ BEGIN IF EXISTS(SELECT 1 FROM authentication_identity_migrations) THEN RAISE EXCEPTION 'identity migration evidence exists; restore matching backup'; END IF; END $$;
DROP TABLE authentication_identity_migrations;
