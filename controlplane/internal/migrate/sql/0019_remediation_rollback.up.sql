-- Add rollback script support to remediation_scripts so failed verifications
-- can trigger an inverse action.
ALTER TABLE remediation_scripts
    ADD COLUMN IF NOT EXISTS rollback_content TEXT,
    ADD COLUMN IF NOT EXISTS rollback_checksum VARCHAR(64);

COMMENT ON COLUMN remediation_scripts.rollback_content IS 'Inverse script body executed when post-remediation verification fails';
COMMENT ON COLUMN remediation_scripts.rollback_checksum IS 'SHA256 checksum of rollback_content';
