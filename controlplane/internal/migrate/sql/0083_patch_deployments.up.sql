-- patch_deployments + node_patch_state — fleet-wide OS package patching
-- (PR 4). One operator action ("deploy patches to N nodes") creates one
-- patch_deployments row plus N node_patch_state rows. Jobs of type
-- patch.deploy_direct are dispatched per node via heartbeat PendingActions
-- (same channel firewall.* uses), and the agent reports outcomes via
-- heartbeat completed_actions, mutating node_patch_state.status from
-- pending → applied | failed.
--
-- mode is currently always 'direct' (apt/dnf/winget on the node itself).
-- Proxy and airgapped modes plus Squid are deferred to a follow-up PR.

CREATE TABLE IF NOT EXISTS patch_deployments (
    id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID         NOT NULL,
    mode               TEXT         NOT NULL DEFAULT 'direct'
                                      CHECK (mode IN ('direct', 'proxy', 'airgapped')),
    status             TEXT         NOT NULL DEFAULT 'pending'
                                      CHECK (status IN ('pending', 'in_progress', 'completed', 'partial', 'failed')),
    target_node_count  INTEGER      NOT NULL DEFAULT 0,
    requested_by       UUID,
    requested_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    summary            JSONB        NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_patch_deployments_tenant
    ON patch_deployments (tenant_id, requested_at DESC);

CREATE TABLE IF NOT EXISTS node_patch_state (
    id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id      UUID         NOT NULL REFERENCES patch_deployments(id) ON DELETE CASCADE,
    node_id            UUID         NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tenant_id          UUID         NOT NULL,
    status             TEXT         NOT NULL DEFAULT 'pending'
                                      CHECK (status IN ('pending', 'applied', 'failed')),
    packages_upgraded  INTEGER,
    log_tail           TEXT,
    error              TEXT,
    job_id             UUID,
    requested_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    applied_at         TIMESTAMPTZ,
    UNIQUE (deployment_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_node_patch_state_node
    ON node_patch_state (node_id, status);
CREATE INDEX IF NOT EXISTS idx_node_patch_state_deployment
    ON node_patch_state (deployment_id);
