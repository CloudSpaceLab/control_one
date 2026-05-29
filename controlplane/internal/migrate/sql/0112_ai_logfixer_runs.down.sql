DROP INDEX IF EXISTS idx_ai_logfixer_actions_run;
DROP INDEX IF EXISTS idx_ai_logfixer_actions_node_pending;
DROP TABLE IF EXISTS ai_logfixer_actions;

DROP INDEX IF EXISTS idx_ai_logfixer_runs_job;
DROP INDEX IF EXISTS idx_ai_logfixer_runs_investigation;
DROP INDEX IF EXISTS idx_ai_logfixer_runs_node;
DROP INDEX IF EXISTS idx_ai_logfixer_runs_tenant_status;
DROP TABLE IF EXISTS ai_logfixer_runs;
