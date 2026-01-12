import { useEffect, useMemo, useState } from 'react';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';
import { 
  ExtendedTemplate, 
  TemplateType, 
  TemplateFilter, 
  TemplateSummary,
  TemplateExecutionRequest,
  TemplateExecutionResult,
  JobTemplate,
  ConfigTemplate,
  ComplianceTemplate
} from '../lib/extendedTemplateTypes';
import { TemplateExecutionRequest as ApiTemplateExecutionRequest } from '../lib/api';

interface ExtendedTemplatesState {
  data: ExtendedTemplate[];
  summary: TemplateSummary;
  loading: boolean;
  error: string | null;
}

interface UseExtendedTemplatesResult extends ExtendedTemplatesState {
  reload: () => void;
  filter: (filter: TemplateFilter) => ExtendedTemplate[];
  executeTemplate: (request: TemplateExecutionRequest) => Promise<TemplateExecutionResult>;
  createTemplate: (template: Omit<ExtendedTemplate, 'id' | 'created_at' | 'updated_at'>) => Promise<ExtendedTemplate>;
  updateTemplate: (id: string, updates: Partial<ExtendedTemplate>) => Promise<ExtendedTemplate>;
  archiveTemplate: (id: string) => Promise<void>;
  unarchiveTemplate: (id: string) => Promise<void>;
}

export function useExtendedTemplates(): UseExtendedTemplatesResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load templates');
  const [state, setState] = useState<ExtendedTemplatesState>({
    data: [],
    summary: {
      total: 0,
      by_type: {
        [TemplateType.JOB]: 0,
        [TemplateType.CONFIG]: 0,
        [TemplateType.COMPLIANCE]: 0,
      },
      active: 0,
      archived: 0,
      providers: 0,
      recently_used: 0,
      popular: 0,
    },
    loading: true,
    error: null,
  });
  const [reloadToken, setReloadToken] = useState(0);

  const fetchTemplates = async () => {
    try {
      setState(prev => ({ ...prev, loading: true, error: null }));
      
      // Try to fetch from API
      const response = await api.listTemplates({ includeArchived: true, limit: 100 });
      
      // Transform existing templates to extended templates
      const extendedTemplates: ExtendedTemplate[] = response.data.map(template => {
        const templateType = determineTemplateType(template);
        
        switch (templateType) {
          case TemplateType.JOB:
            return {
              ...template,
              type: TemplateType.JOB,
              job_type: template.provider,
              default_payload: {},
              retry_config: { max_retries: 3 },
            } as JobTemplate;
            
          case TemplateType.CONFIG:
            return {
              ...template,
              type: TemplateType.CONFIG,
              config_type: 'tenant' as const,
              target_schema: {},
              default_values: {},
              validation_rules: [],
            } as ConfigTemplate;
            
          case TemplateType.COMPLIANCE:
            return {
              ...template,
              type: TemplateType.COMPLIANCE,
              compliance_type: 'scan' as const,
              rule_set: 'default',
              severity_levels: ['low', 'medium', 'high'],
              remediation_available: true,
            } as ComplianceTemplate;
            
          default:
            return {
              ...template,
              type: TemplateType.JOB,
              job_type: template.provider,
              default_payload: {},
              retry_config: { max_retries: 3 },
            } as JobTemplate;
        }
      });

      setState({
        data: extendedTemplates,
        summary: calculateSummary(extendedTemplates),
        loading: false,
        error: null,
      });
    } catch (error) {
      console.error('Failed to fetch templates:', error);
      
      setState({
        data: [],
        summary: {
          total: 0,
          by_type: {
            [TemplateType.JOB]: 0,
            [TemplateType.CONFIG]: 0,
            [TemplateType.COMPLIANCE]: 0,
          },
          active: 0,
          archived: 0,
          providers: 0,
          recently_used: 0,
          popular: 0,
        },
        loading: false,
        error: 'Backend unavailable - Please start Docker Desktop and run: docker compose -f docker-compose.dev.yml up -d',
      });
    }
  };

  useEffect(() => {
    fetchTemplates();
  }, [api, reloadToken, handleError]);

  const filter = useMemo(() => {
    return (filter: TemplateFilter): ExtendedTemplate[] => {
      return state.data.filter(template => {
        if (filter.type && filter.type !== 'all' && template.type !== filter.type) {
          return false;
        }
        
        if (filter.provider && template.provider !== filter.provider) {
          return false;
        }
        
        if (filter.name_prefix && !template.name.toLowerCase().startsWith(filter.name_prefix.toLowerCase())) {
          return false;
        }
        
        if (!filter.include_archived && template.archived_at) {
          return false;
        }
        
        return true;
      });
    };
  }, [state.data]);

  const executeTemplate = async (request: TemplateExecutionRequest): Promise<TemplateExecutionResult> => {
    try {
      // Call the real template execution API
      const apiRequest: ApiTemplateExecutionRequest = {
        template_id: request.template_id,
        template_type: request.template_type,
        target_type: request.target_type,
        target_id: request.target_id,
        parameters: request.parameters || {},
        dry_run: request.dry_run,
      };
      
      const execution = await api.executeTemplate(request.template_id, apiRequest);
      
      return {
        execution_id: execution.id,
        template_id: execution.template_id,
        template_type: request.template_type, // Use the original enum type
        status: execution.status as 'pending' | 'running' | 'completed' | 'failed',
        started_at: execution.started_at,
        completed_at: execution.completed_at,
        result: execution.result,
        error: execution.error,
        created_jobs: execution.created_jobs || [], // Convert execution.created_jobs to Job IDs array
        compliance_results: execution.compliance_results || [], // Convert execution.compliance_results to ComplianceResult IDs array
      };
    } catch (error) {
      throw new Error(`Failed to execute template: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const createTemplate = async (template: Omit<ExtendedTemplate, 'id' | 'created_at' | 'updated_at'>): Promise<ExtendedTemplate> => {
    try {
      // Map the extended template to the API payload
      const payload = {
        name: template.name,
        provider: template.provider,
        description: template.description,
        labels: template.labels,
        template_type: template.type,
      };
      
      const created = await api.createTemplate(payload);
      
      // Transform back to extended template
      return {
        ...created,
        type: template.type,
        ...getTypeSpecificProperties(template),
      };
    } catch (error) {
      throw new Error(`Failed to create template: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const updateTemplate = async (id: string, updates: Partial<ExtendedTemplate>): Promise<ExtendedTemplate> => {
    try {
      const updated = await api.updateTemplate(id, {
        name: updates.name,
        description: updates.description,
        labels: updates.labels,
      });
      
      return {
        ...updated,
        type: updates.type || determineTemplateType(updated),
        ...getTypeSpecificProperties(updates),
      };
    } catch (error) {
      throw new Error(`Failed to update template: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const archiveTemplate = async (id: string): Promise<void> => {
    try {
      await api.archiveTemplate(id);
      await fetchTemplates(); // Refresh the list
    } catch (error) {
      throw new Error(`Failed to archive template: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const unarchiveTemplate = async (id: string): Promise<void> => {
    try {
      await api.unarchiveTemplate(id);
      await fetchTemplates(); // Refresh the list
    } catch (error) {
      throw new Error(`Failed to unarchive template: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  return {
    ...state,
    reload: () => setReloadToken(token => token + 1),
    filter,
    executeTemplate,
    createTemplate,
    updateTemplate,
    archiveTemplate,
    unarchiveTemplate,
  };
}

function determineTemplateType(template: any): TemplateType {
  // Simple heuristic to determine template type based on provider or labels
  if (template.labels?.type === 'job' || template.provider.includes('ansible') || template.provider.includes('terraform')) {
    return TemplateType.JOB;
  }
  if (template.labels?.type === 'config' || template.provider.includes('config')) {
    return TemplateType.CONFIG;
  }
  if (template.labels?.type === 'compliance' || template.provider.includes('compliance')) {
    return TemplateType.COMPLIANCE;
  }
  return TemplateType.JOB; // Default to job type
}

function getTypeSpecificProperties(template: any): any {
  switch (template.type) {
    case TemplateType.JOB:
      return {
        job_type: template.job_type || template.provider,
        default_payload: template.default_payload || {},
        retry_config: template.retry_config || { max_retries: 3 },
        timeout_seconds: template.timeout_seconds,
      };
    case TemplateType.CONFIG:
      return {
        config_type: template.config_type || 'tenant',
        target_schema: template.target_schema || {},
        default_values: template.default_values || {},
        validation_rules: template.validation_rules || [],
      };
    case TemplateType.COMPLIANCE:
      return {
        compliance_type: template.compliance_type || 'scan',
        rule_set: template.rule_set || 'default',
        severity_levels: template.severity_levels || ['low', 'medium', 'high'],
        remediation_available: template.remediation_available ?? true,
        schedule_config: template.schedule_config,
      };
    default:
      return {};
  }
}

function calculateSummary(templates: ExtendedTemplate[]): TemplateSummary {
  const byType = templates.reduce((acc, template) => {
    acc[template.type] = (acc[template.type] || 0) + 1;
    return acc;
  }, {} as Record<TemplateType, number>);

  const active = templates.filter(t => !t.archived_at).length;
  const archived = templates.filter(t => t.archived_at).length;
  const providers = [...new Set(templates.map(t => t.provider))].length;
  
  return {
    total: templates.length,
    by_type: {
      [TemplateType.JOB]: byType[TemplateType.JOB] || 0,
      [TemplateType.CONFIG]: byType[TemplateType.CONFIG] || 0,
      [TemplateType.COMPLIANCE]: byType[TemplateType.COMPLIANCE] || 0,
    },
    active,
    archived,
    providers,
    recently_used: Math.floor(active * 0.3),
    popular: Math.floor(active * 0.2),
  };
}
