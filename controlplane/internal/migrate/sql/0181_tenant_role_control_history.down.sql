DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM tenant_role_control_events) THEN
  RAISE EXCEPTION 'retained tenant access history prevents destructive rollback';
 END IF;
END $$;
DROP TABLE tenant_role_control_events;
DROP TABLE tenant_role_controls;
