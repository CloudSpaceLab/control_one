-- Add template type column to provisioning_templates
ALTER TABLE provisioning_templates 
ADD COLUMN template_type TEXT NOT NULL DEFAULT 'job';

-- Add index for template type filtering
CREATE INDEX IF NOT EXISTS provisioning_templates_type_idx 
    ON provisioning_templates(template_type);

-- Add template execution tracking table
CREATE TABLE IF NOT EXISTS template_executions (
    id UUID PRIMARY KEY,
    template_id UUID NOT NULL REFERENCES provisioning_templates(id) ON DELETE CASCADE,
    template_type TEXT NOT NULL,
    target_type TEXT NOT NULL, -- 'tenant', 'node', 'global'
    target_id UUID,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'running', 'completed', 'failed'
    execution_result JSONB,
    error_message TEXT,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

-- Indexes for template executions
CREATE INDEX IF NOT EXISTS template_executions_template_id_idx 
    ON template_executions(template_id);
CREATE INDEX IF NOT EXISTS template_executions_status_idx 
    ON template_executions(status);
CREATE INDEX IF NOT EXISTS template_executions_target_idx 
    ON template_executions(target_type, target_id);

-- Add created jobs tracking for template executions
CREATE TABLE IF NOT EXISTS template_execution_jobs (
    id UUID PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES template_executions(id) ON DELETE CASCADE,
    job_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS template_execution_jobs_execution_id_idx 
    ON template_execution_jobs(execution_id);

-- Add compliance results tracking for template executions
CREATE TABLE IF NOT EXISTS template_execution_compliance (
    id UUID PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES template_executions(id) ON DELETE CASCADE,
    compliance_result_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS template_execution_compliance_execution_id_idx 
    ON template_execution_compliance(execution_id);

-- Update existing templates to have 'job' type by default
UPDATE provisioning_templates 
SET template_type = 'job' 
WHERE template_type IS NULL OR template_type = '';

-- Add constraint to ensure valid template types
ALTER TABLE provisioning_templates 
ADD CONSTRAINT provisioning_templates_type_check 
    CHECK (template_type IN ('job', 'config', 'compliance'));
