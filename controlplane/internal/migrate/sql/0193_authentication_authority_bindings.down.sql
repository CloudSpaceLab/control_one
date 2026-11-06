DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM authentication_authority_bindings) THEN
  RAISE EXCEPTION 'refusing to discard authentication authority bindings';
 END IF;
END $$;
DROP TABLE authentication_authority_bindings;
