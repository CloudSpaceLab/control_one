DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM patch_organization_approval_bindings) OR EXISTS(SELECT 1 FROM patch_organization_history) THEN
  RAISE EXCEPTION 'retained patch authorization bindings prevent destructive rollback';
 END IF;
END $$;
DROP TABLE patch_organization_approval_bindings;
ALTER TABLE patch_organization_history DROP COLUMN bound_controls;
ALTER TABLE patch_organization_policy DROP COLUMN bound_controls;
