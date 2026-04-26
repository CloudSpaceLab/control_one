-- Enable pg_stat_statements so the dbquery scraper can read aggregated
-- query statistics from this Postgres instance. Wrapped in a DO block
-- so the migration succeeds even when the postgresql-contrib package
-- isn't installed (extension file missing). The dbquery scraper logs
-- and idles when the view is unavailable.
DO $$ BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'pg_stat_statements extension unavailable: %', SQLERRM;
END $$;
