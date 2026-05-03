DROP INDEX IF EXISTS idx_finacle_profiles_shift;
DROP INDEX IF EXISTS idx_finacle_profiles_branch;
DROP INDEX IF EXISTS idx_finacle_profiles_tenant;
DROP TABLE IF EXISTS finacle_profiles;

DROP INDEX IF EXISTS idx_finacle_shift_configs_branch;
DROP INDEX IF EXISTS idx_finacle_shift_configs_tenant;
DROP TABLE IF EXISTS finacle_shift_configs;

DROP INDEX IF EXISTS idx_finacle_connections_tenant;
DROP TABLE IF EXISTS finacle_connections;
