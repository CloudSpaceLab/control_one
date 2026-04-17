ALTER TABLE compliance_results ADD COLUMN IF NOT EXISTS remediation_job_id UUID REFERENCES jobs(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_compliance_results_remediation ON compliance_results(remediation_job_id) WHERE remediation_job_id IS NOT NULL;
