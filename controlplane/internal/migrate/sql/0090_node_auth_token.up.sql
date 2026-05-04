-- Add per-node auth token for agent authentication.
-- Agents send this as "Authorization: Bearer <token>" on heartbeat and
-- telemetry requests, replacing the mTLS-cert-CN dependency.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS auth_token TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS nodes_auth_token_idx ON nodes (auth_token) WHERE auth_token IS NOT NULL;
