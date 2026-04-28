-- 0067_ip_enrichment_cache: persistent cache for IP intelligence lookups
-- (akyriako/ipquery + AbuseIPDB direct). Hits are reused across investigators
-- and survive restarts so we don't burn through external rate limits.
CREATE TABLE IF NOT EXISTS ip_enrichment_cache (
    addr        TEXT PRIMARY KEY,
    payload     JSONB NOT NULL,
    source      TEXT NOT NULL,
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS ip_enrichment_cache_expires_idx
    ON ip_enrichment_cache (expires_at);
