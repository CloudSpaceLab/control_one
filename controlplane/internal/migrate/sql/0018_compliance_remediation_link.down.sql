DROP INDEX IF EXISTS idx_compliance_results_remediation;
ALTER TABLE compliance_results DROP COLUMN IF EXISTS remediation_job_id;
