-- asset_cidrs declares which CIDR ranges a tenant considers their own
-- assets. Used by the IP enrichment endpoint to tag IPs ASSET / INTERNAL
-- / EXTERNAL during investigation.
CREATE TABLE IF NOT EXISTS asset_cidrs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    cidr        CIDR NOT NULL,
    name        TEXT,
    owner       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_asset_cidrs_tenant ON asset_cidrs (tenant_id);
-- GIST index over inet_ops accelerates `>>` (cidr contains ip) lookups.
CREATE INDEX IF NOT EXISTS idx_asset_cidrs_cidr_gist
    ON asset_cidrs USING GIST (cidr inet_ops);
