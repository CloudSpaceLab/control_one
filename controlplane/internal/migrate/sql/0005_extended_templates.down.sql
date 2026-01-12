-- Drop template execution tables
DROP TABLE IF EXISTS template_execution_compliance;
DROP TABLE IF EXISTS template_execution_jobs;
DROP TABLE IF EXISTS template_executions;

-- Remove template type column and constraints
ALTER TABLE provisioning_templates 
DROP CONSTRAINT IF EXISTS provisioning_templates_type_check;

DROP INDEX IF EXISTS provisioning_templates_type_idx;

ALTER TABLE provisioning_templates 
DROP COLUMN IF EXISTS template_type;
