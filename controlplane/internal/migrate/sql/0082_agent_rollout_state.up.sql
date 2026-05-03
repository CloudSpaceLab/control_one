-- agent_rollout_state controls fraction-based staged rollout of agent
-- self-updates per tenant. One row per tenant. Operators tune rollout_pct
-- across waves (e.g., 5% → 25% → 100%); the agent only proceeds with
-- self-update when its stable bucket (crc32(node_id) mod 100) is less than
-- rollout_pct AND target_release_seq > current_release_seq persisted on disk.
--
-- target_release_seq is monotonic — the gate that prevents downgrade. The
-- agent persists the highest release_seq it has ever seen and refuses
-- anything lower.
--
-- target_version is informational (semver string operators recognise).
--
-- paused, when true, halts all updates regardless of rollout_pct — used as
-- an emergency brake when a bad binary is in flight.

CREATE TABLE IF NOT EXISTS agent_rollout_state (
    tenant_id          UUID         PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    target_release_seq INTEGER      NOT NULL DEFAULT 0,
    target_version     TEXT         NOT NULL DEFAULT '',
    rollout_pct        INTEGER      NOT NULL DEFAULT 0 CHECK (rollout_pct BETWEEN 0 AND 100),
    paused             BOOLEAN      NOT NULL DEFAULT FALSE,
    updated_by         UUID,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- No tenant filter on the index — primary key already covers (tenant_id).
