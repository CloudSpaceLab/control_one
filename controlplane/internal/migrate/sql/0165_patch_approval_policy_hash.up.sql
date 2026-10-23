ALTER TABLE patch_approvals ADD COLUMN policy_hash TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN patch_approvals.policy_hash IS 'SHA-256 of reviewed patch policy and resource identities; empty legacy approvals must be reissued';
