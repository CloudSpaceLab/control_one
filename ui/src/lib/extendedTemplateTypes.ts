import { TemplateVersion } from './api';

export enum TemplateType {
  JOB = 'job',
  CONFIG = 'config',
  COMPLIANCE = 'compliance'
}

export interface BaseTemplate {
  id: string;
  name: string;
  type: TemplateType;
  provider: string;
  description?: string;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
  archived_at?: string;
  promoted_version_id?: string;
  promoted_version?: TemplateVersion;
}

export interface JobTemplate extends BaseTemplate {
  type: TemplateType.JOB;
  job_type: string;
  default_payload?: Record<string, unknown>;
  retry_config?: {
    max_retries: number;
    retry_delay_seconds?: number;
  };
  timeout_seconds?: number;
}

export interface ConfigTemplate extends BaseTemplate {
  type: TemplateType.CONFIG;
  config_type: 'tenant' | 'node' | 'global';
  target_schema?: Record<string, unknown>;
  default_values?: Record<string, unknown>;
  validation_rules?: ValidationRule[];
}

export interface ComplianceTemplate extends BaseTemplate {
  type: TemplateType.COMPLIANCE;
  compliance_type: 'scan' | 'remediation' | 'policy';
  rule_set: string;
  severity_levels: string[];
  remediation_available: boolean;
  schedule_config?: {
    enabled: boolean;
    cron_expression?: string;
    timezone?: string;
  };
}

export type ExtendedTemplate = JobTemplate | ConfigTemplate | ComplianceTemplate;

export interface ValidationRule {
  field: string;
  required: boolean;
  type: 'string' | 'number' | 'boolean' | 'object' | 'array';
  min_length?: number;
  max_length?: number;
  pattern?: string;
  allowed_values?: unknown[];
}

export interface TemplateExecutionRequest {
  template_id: string;
  template_type: TemplateType;
  target_type: 'tenant' | 'node' | 'global';
  target_id?: string;
  parameters?: Record<string, unknown>;
  dry_run?: boolean;
}

export interface TemplateExecutionResult {
  execution_id: string;
  template_id: string;
  template_type: TemplateType;
  status: 'pending' | 'running' | 'completed' | 'failed';
  started_at?: string;
  completed_at?: string;
  result?: unknown;
  error?: string;
  created_jobs?: string[]; // Job IDs as strings
  compliance_results?: string[]; // Compliance result IDs as strings
}

export interface TemplateWizardStep {
  id: string;
  title: string;
  description: string;
  type: 'template-selection' | 'target-selection' | 'parameter-configuration' | 'review' | 'execution';
  component?: string;
  validation?: (data: Record<string, unknown>) => boolean;
}

export interface TemplateWizardConfig {
  id: string;
  name: string;
  description: string;
  template_types: TemplateType[];
  steps: TemplateWizardStep[];
  allow_multi_target: boolean;
  quick_actions: QuickAction[];
}

export interface QuickAction {
  id: string;
  name: string;
  description: string;
  icon: string;
  template_id: string;
  preset_parameters?: Record<string, unknown>;
  target_type: 'tenant' | 'node';
  requires_selection: boolean;
}

export interface TemplateSummary {
  total: number;
  by_type: Record<TemplateType, number>;
  active: number;
  archived: number;
  providers: number;
  recently_used: number;
  popular: number;
}

export interface TemplateFilter {
  type?: TemplateType | 'all';
  provider?: string;
  name_prefix?: string;
  include_archived?: boolean;
  labels?: Record<string, string>;
  limit?: number;
  offset?: number;
}

export interface TemplateAction {
  id: string;
  name: string;
  description: string;
  icon: string;
  requires_confirmation: boolean;
  execution: (template: ExtendedTemplate, params?: Record<string, unknown>) => Promise<void>;
}
