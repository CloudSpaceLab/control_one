-- Post-remediation verification + rollback linkage on compliance_results.
ALTER TABLE compliance_results
    ADD COLUMN IF NOT EXISTS verified BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS verification_job_id UUID REFERENCES jobs(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS rollback_job_id UUID REFERENCES jobs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_results_verification
    ON compliance_results (verification_job_id)
    WHERE verification_job_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_results_rollback
    ON compliance_results (rollback_job_id)
    WHERE rollback_job_id IS NOT NULL;

COMMENT ON COLUMN compliance_results.verified IS 'True when post-remediation verify re-scan passed';
COMMENT ON COLUMN compliance_results.verification_job_id IS 'Job id of the compliance.verify re-scan triggered after remediation';
COMMENT ON COLUMN compliance_results.rollback_job_id IS 'Job id of the remediation.rollback triggered when verification failed';
