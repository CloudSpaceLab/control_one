DROP INDEX IF EXISTS nodes_auth_token_idx;
ALTER TABLE nodes DROP COLUMN IF EXISTS auth_token;
