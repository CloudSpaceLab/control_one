DROP INDEX IF EXISTS idx_compliance_results_rollback;
DROP INDEX IF EXISTS idx_compliance_results_verification;
ALTER TABLE compliance_results DROP COLUMN IF EXISTS rollback_job_id;
ALTER TABLE compliance_results DROP COLUMN IF EXISTS verification_job_id;
ALTER TABLE compliance_results DROP COLUMN IF EXISTS verified;
