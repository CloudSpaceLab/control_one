DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_change_events) THEN
 RAISE EXCEPTION 'role change audit evidence exists; restore a matching backup instead of destructive rollback';
 END IF;
END $$;
DROP TABLE global_role_change_events;
