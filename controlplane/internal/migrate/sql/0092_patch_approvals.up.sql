-- Patch approval gate (S4 row 8 — fix/c1-s4-patch-gate, bugs §3.1).
--
-- The compliance pipeline already has an approve→dispatch loop via
-- remediation_approvals (migration 0030), but that table is keyed on
-- script_id which patch deploys do not have. patch_approvals is the
-- patch-domain twin: deployment_id + node_id + mode, with the same
-- pending → approved | denied | expired lifecycle.
--
-- runPatchSafetyGates writes a row when the tenant's
-- patch_requires_approval flag is true; the operator approves via
-- POST /api/v1/patch/approvals/:id/approve and the server re-dispatches
-- the underlying patch.deploy_* job. Denied flips the node to
-- gate_rejected and drops the dispatch.

CREATE TABLE IF NOT EXISTS patch_approvals (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL REFERENCES tenants(id)        ON DELETE CASCADE,
    deployment_id   UUID         NOT NULL REFERENCES patch_deployments(id) ON DELETE CASCADE,
    node_id         UUID         NOT NULL REFERENCES nodes(id)          ON DELETE CASCADE,
    mode            TEXT         NOT NULL
                                    CHECK (mode IN ('direct','proxy','airgapped')),
    proxy_id        UUID,
    window_id       UUID,
    status          TEXT         NOT NULL DEFAULT 'pending'
                                    CHECK (status IN ('pending','approved','denied','expired')),
    approved_by     UUID         REFERENCES users(id),
    approved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ  NOT NULL,
    UNIQUE (deployment_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_patch_approvals_tenant_status
    ON patch_approvals (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_patch_approvals_deployment
    ON patch_approvals (deployment_id);
CREATE INDEX IF NOT EXISTS idx_patch_approvals_expires_at
    ON patch_approvals (expires_at);

COMMENT ON TABLE  patch_approvals IS 'Per-(deployment,node) operator approval for fleet patch deploys';
COMMENT ON COLUMN patch_approvals.mode IS 'Resolved per-node patch mode (direct|proxy|airgapped) — preserved so dispatch on approval matches the original request';

-- Per-tenant toggle exposing the proper approve→dispatch loop on/off.
-- Default ON so production tenants land on the safe path; operator can
-- explicitly opt out for tenants that never want the gate (e.g. lab
-- environments).
ALTER TABLE tenant_remediation_config
    ADD COLUMN IF NOT EXISTS patch_requires_approval BOOLEAN NOT NULL DEFAULT true;

COMMENT ON COLUMN tenant_remediation_config.patch_requires_approval IS 'When true (default) patch deploys flow through the approve→dispatch loop; when false the legacy immediate-dispatch behaviour applies';
