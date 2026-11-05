-- Distinct lifetimes cannot be represented safely in the older schema.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM global_role_sources g GROUP BY assignment_id
 HAVING count(DISTINCT COALESCE(expires_at::text,'permanent'))>1) THEN
  RAISE EXCEPTION 'distinct grant-source expiries exist; reconcile them before rollback';
 END IF;
END $$;
ALTER TABLE global_role_sources DROP COLUMN expires_at;
