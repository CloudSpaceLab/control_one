-- Sprint 3 (Worktree 1, Pillar 2.6): CP-signed remediation scripts.
-- The control plane signs each script with the CP CA private key on write.
-- The remediation engine (run by the CP worker acting on behalf of each node)
-- verifies the signature against the CP CA public key before exec so tampered
-- or unsigned scripts are refused.
--
-- `signature` stores the base64 DER-encoded ECDSA signature over
--     sha256(content || "\n" || platform || "\n" || version)
-- `signature_algorithm` records which alg was used so the verifier can pick
-- the right primitive. Default is the CP CA alg (ECDSA P-256 + SHA-256).
--
-- Existing rows written before Sprint 3 are left NULL; the CP startup backfill
-- signs them as soon as a CA key is configured. The engine treats missing
-- signatures as a verification failure when `require_signature` is on.

ALTER TABLE remediation_scripts
    ADD COLUMN IF NOT EXISTS signature             TEXT,
    ADD COLUMN IF NOT EXISTS signature_algorithm   TEXT;

COMMENT ON COLUMN remediation_scripts.signature
    IS 'Base64 DER-encoded signature over sha256(content||platform||version); produced by the CP CA key on create/update';
COMMENT ON COLUMN remediation_scripts.signature_algorithm
    IS 'Signature algorithm identifier (e.g. ecdsa-p256-sha256); must match the CP CA key type';
