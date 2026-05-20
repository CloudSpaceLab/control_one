DROP INDEX IF EXISTS access_entitlements_created_at_idx;

ALTER TABLE access_entitlements
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;
