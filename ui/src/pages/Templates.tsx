import { FormEvent, useEffect, useMemo, useState } from 'react';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useExtendedTemplates } from '../hooks/useExtendedTemplates';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { 
  EnterpriseLayout, 
  ExecutiveOverview, 
  ManagementPanel, 
  ActionZone,
  ContentGrid 
} from '../components/EnterpriseLayout';
import { 
  TemplateType, 
  TemplateFilter,
  summarizeTemplates,
  filterTemplates,
  getTemplateProviders,
  getTemplateIcon,
  getTemplateTypeLabel,
  getTemplateStatus
} from '../lib/extendedTemplateUtils';
import './EnterpriseLayout.css';

// Import the enum values for runtime use
const JobType = 'job';
const ConfigType = 'config';
const ComplianceType = 'compliance';

import {
  parseTemplateLabels,
} from '../lib/templateUtils';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function Templates(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const [limit] = useState(20);
  const [offset, setOffset] = useState(0);
  const [providerFilter, setProviderFilter] = useState('');
  const [nameFilter, setNameFilter] = useState('');
  const [typeFilter, setTypeFilter] = useState<TemplateType | 'all'>('all');
  const [includeArchived, setIncludeArchived] = useState(false);
  
  // Use extended templates system
  const { data: extendedTemplates, loading, error, reload } = useExtendedTemplates();
  
  // Filter templates
  const filter: TemplateFilter = {
    type: typeFilter,
    provider: providerFilter.trim() || undefined,
    name_prefix: nameFilter.trim() || undefined,
    include_archived: includeArchived,
    limit,
    offset,
  };
  
  const filteredTemplates = filterTemplates(extendedTemplates, filter);
  
  // Calculate summary from filtered templates
  const summary = useMemo(() => {
    return summarizeTemplates(extendedTemplates);
  }, [extendedTemplates]);
  
  // Get providers for filter dropdown
  const availableProviders = useMemo(() => {
    return getTemplateProviders(extendedTemplates);
  }, [extendedTemplates]);

  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null);

  useEffect(() => {
    if (filteredTemplates.length === 0) {
      setSelectedTemplateId(null);
      return;
    }
    if (!selectedTemplateId && filteredTemplates.length > 0) {
      setSelectedTemplateId(filteredTemplates[0].id);
    }
  }, [filteredTemplates, selectedTemplateId]);

  const selectedTemplate = useMemo(
    () => extendedTemplates.find((t) => t.id === selectedTemplateId) ?? null,
    [extendedTemplates, selectedTemplateId],
  );

  // Pagination for filtered templates
  const pagination = useMemo(() => {
    return {
      total: extendedTemplates.length,
      limit,
      offset,
      hasMore: offset + limit < extendedTemplates.length,
    };
  }, [extendedTemplates, limit, offset]);

  // Template versions - properly typed
  if (selectedTemplateId) {
    useTemplateVersions({ templateId: selectedTemplateId });
  }

  // Form state
  const [createName, setCreateName] = useState('');
  const [createProvider, setCreateProvider] = useState('');
  const [createDescription, setCreateDescription] = useState('');
  const [createLabels, setCreateLabels] = useState('');
  const [createType, setCreateType] = useState<string>(JobType);
  
  // Job template specific fields
  const [createJobType, setCreateJobType] = useState('');
  const [createTimeout, setCreateTimeout] = useState('');
  const [createMaxRetries, setCreateMaxRetries] = useState('');
  const [createRetryDelay, setCreateRetryDelay] = useState('');
  const [createExecutionMode, setCreateExecutionMode] = useState<'sequential' | 'parallel'>('sequential');
  const [createEnvironment, setCreateEnvironment] = useState('');
  const [createRequirements, setCreateRequirements] = useState('');
  
  // Config template specific fields
  const [createConfigType, setCreateConfigType] = useState<'tenant' | 'node' | 'global'>('node');
  const [createDefaultValues, setCreateDefaultValues] = useState('');
  
  // Compliance template specific fields
  const [createComplianceType, setCreateComplianceType] = useState<'scan' | 'remediation' | 'policy'>('scan');
  const [createRuleSet, setCreateRuleSet] = useState('');
  const [createSeverityLevels, setCreateSeverityLevels] = useState('');
  const [createRemediationAvailable, setCreateRemediationAvailable] = useState(false);
  const [createComplianceFramework, setCreateComplianceFramework] = useState('');
  const [createSchedule, setCreateSchedule] = useState('');
  const [createNotificationThreshold, setCreateNotificationThreshold] = useState('');
  
  // File upload states
  const [playbookFile, setPlaybookFile] = useState<File | null>(null);
  const [terraformFile, setTerraformFile] = useState<File | null>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [isUploading, setIsUploading] = useState(false);

  const [updateName, setUpdateName] = useState(selectedTemplate?.name ?? '');
  const [updateDescription, setUpdateDescription] = useState(selectedTemplate?.description ?? '');
  const [updateLabels] = useState(
    selectedTemplate?.labels ? JSON.stringify(selectedTemplate.labels) : ''
  );

  const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();

  const [isCreating, setIsCreating] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const [isArchiving, setIsArchiving] = useState(false);
  const [isUnarchiving, setIsUnarchiving] = useState(false);

  // File upload handlers
  const handleFileUpload = async (file: File, type: 'playbook' | 'terraform') => {
    setIsUploading(true);
    setUploadProgress(0);
    
    try {
      // Simulate file upload progress
      const progressInterval = setInterval(() => {
        setUploadProgress(prev => {
          if (prev >= 90) {
            clearInterval(progressInterval);
            return 90;
          }
          return prev + 10;
        });
      }, 100);

      // Simulate upload delay
      await new Promise(resolve => setTimeout(resolve, 1000));
      
      clearInterval(progressInterval);
      setUploadProgress(100);
      
      if (type === 'playbook') {
        setPlaybookFile(file);
      } else {
        setTerraformFile(file);
      }
      
      showSuccess(`${type === 'playbook' ? 'Playbook' : 'Terraform file'} uploaded successfully.`);
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to upload ${type}.`;
      showError(message);
    } finally {
      setIsUploading(false);
      setTimeout(() => setUploadProgress(0), 1000);
    }
  };

  const handlePlaybookUpload = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {
      if (file.name.endsWith('.yml') || file.name.endsWith('.yaml') || file.name.endsWith('.playbook')) {
        handleFileUpload(file, 'playbook');
      } else {
        showError('Please upload a valid playbook file (.yml, .yaml, .playbook)');
      }
    }
  };

  const handleTerraformUpload = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {
      if (file.name.endsWith('.tf') || file.name.endsWith('.tf.json')) {
        handleFileUpload(file, 'terraform');
      } else {
        showError('Please upload a valid Terraform file (.tf, .tf.json)');
      }
    }
  };

  const handleCreateTemplate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = createName.trim();
    const trimmedProvider = createProvider.trim();
    const trimmedDescription = createDescription.trim();

    if (!trimmedName) {
      showError('Template name is required');
      return;
    }
    if (!trimmedProvider) {
      showError('Provider is required');
      return;
    }

    setIsCreating(true);
    reset();

    try {
      const labels = createLabels.trim() ? parseTemplateLabels(createLabels) : undefined;
      
      // Build base payload
      const basePayload = {
        name: trimmedName,
        provider: trimmedProvider,
        description: trimmedDescription || undefined,
        labels,
      };
      
      // Add type-specific fields
      let payload = basePayload;
      
      if (createType === JobType) {
        payload = {
          ...basePayload,
          labels: {
            ...(labels || {}),
            template_type: 'job',
            job_type: createJobType.trim() || 'provision',
            execution_mode: createExecutionMode,
            ...(createTimeout && { timeout_seconds: createTimeout }),
            ...(createMaxRetries && { max_retries: createMaxRetries }),
            ...(createRetryDelay && { retry_delay_seconds: createRetryDelay }),
            ...(createEnvironment.trim() && { environment: createEnvironment }),
            ...(createRequirements.trim() && { requirements: createRequirements }),
            ...(playbookFile && { playbook_file: playbookFile.name }),
          },
        };
      } else if (createType === ConfigType) {
        payload = {
          ...basePayload,
          labels: {
            ...(labels || {}),
            template_type: 'config',
            config_type: createConfigType,
            ...(createDefaultValues.trim() && { default_values: createDefaultValues }),
            ...(terraformFile && { terraform_file: terraformFile.name }),
          },
        };
      } else if (createType === ComplianceType) {
        payload = {
          ...basePayload,
          labels: {
            ...(labels || {}),
            template_type: 'compliance',
            compliance_type: createComplianceType,
            rule_set: createRuleSet.trim() || 'default',
            ...(createSeverityLevels.trim() && { severity_levels: createSeverityLevels }),
            remediation_available: createRemediationAvailable.toString(),
            ...(createComplianceFramework.trim() && { compliance_framework: createComplianceFramework }),
            ...(createSchedule.trim() && { schedule: createSchedule }),
            ...(createNotificationThreshold.trim() && { notification_threshold: createNotificationThreshold }),
          },
        };
      }

      await api.createTemplate(payload);

      showSuccess(`Template "${trimmedName}" created successfully.`);
      showToast('Template created successfully.', 'success');
      
      // Reset all form fields
      setCreateName('');
      setCreateProvider('');
      setCreateDescription('');
      setCreateLabels('');
      setCreateType(JobType);
      setCreateJobType('');
      setCreateTimeout('');
      setCreateMaxRetries('');
      setCreateRetryDelay('');
      setCreateExecutionMode('sequential');
      setCreateEnvironment('');
      setCreateRequirements('');
      setCreateConfigType('node');
      setCreateDefaultValues('');
      setCreateComplianceType('scan');
      setCreateRuleSet('');
      setCreateSeverityLevels('');
      setCreateRemediationAvailable(false);
      setCreateComplianceFramework('');
      setCreateSchedule('');
      setCreateNotificationThreshold('');
      setPlaybookFile(null);
      setTerraformFile(null);
      
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create template.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setIsCreating(false);
    }
  };

  const handleUpdateTemplate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selectedTemplate) return;

    const trimmedName = updateName.trim();
    const trimmedDescription = updateDescription.trim();

    if (!trimmedName) {
      showError('Template name is required');
      return;
    }

    setIsUpdating(true);
    try {
      const labels = parseTemplateLabels(updateLabels);

      await api.updateTemplate(selectedTemplate.id, {
        name: trimmedName,
        description: trimmedDescription,
        labels,
      });

      showSuccess('Template updated successfully.');
      showToast('Template updated successfully.', 'success');
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to update template.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setIsUpdating(false);
    }
  };

  const handleArchiveTemplate = async () => {
    if (!selectedTemplate) return;

    const confirmed = window.confirm(
      `Archive template "${selectedTemplate.name}"? This will hide it from the template list.`
    );
    if (!confirmed) return;

    setIsArchiving(true);
    try {
      await api.archiveTemplate(selectedTemplate.id);
      showSuccess('Template archived successfully.');
      showToast('Template archived successfully.', 'success');
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to archive template.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setIsArchiving(false);
    }
  };

  const handleUnarchiveTemplate = async () => {
    if (!selectedTemplate) return;

    setIsUnarchiving(true);
    try {
      await api.unarchiveTemplate(selectedTemplate.id);
      showSuccess('Template unarchived successfully.');
      showToast('Template unarchived successfully.', 'success');
      reload();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to unarchive template.';
      showError(message);
      showToast(message, 'error');
    } finally {
      setIsUnarchiving(false);
    }
  };

  return (
    <EnterpriseLayout variant="management">
      {/* Executive Overview */}
      <ExecutiveOverview 
        title="📋 Template Management"
        subtitle="Manage job templates for provisioning and compliance workflows"
      >
        <article className="stat-card">
          <span className="muted">Total Templates</span>
          <strong>{summary.total}</strong>
          <small className="muted">All templates</small>
        </article>
        <article className="stat-card">
          <span className="muted">Active</span>
          <strong>{summary.active}</strong>
          <small className="muted">Available for use</small>
        </article>
        <article className="stat-card">
          <span className="muted">Providers</span>
          <strong>{summary.providers}</strong>
          <small className="muted">Integration types</small>
        </article>
        <article className="stat-card">
          <span className="muted">Compliance</span>
          <strong>{(extendedTemplates.filter(t => t.type === 'compliance')).length}</strong>
          <small className="muted">Security templates</small>
        </article>
      </ExecutiveOverview>

      <div className="management-dashboard">
        {/* Error/Warning Banner */}
        {error && (
          <div className="form-error" style={{ marginBottom: '2rem', padding: '1rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
              <span style={{ fontSize: '1.2rem' }}>⚠️</span>
              <strong>Backend Connection Error</strong>
            </div>
            <div style={{ fontSize: '0.9rem', lineHeight: '1.4' }}>
              {error}
            </div>
            <div style={{ fontSize: '0.8rem', marginTop: '0.5rem', opacity: '0.8' }}>
              Template creation and management will be unavailable until the backend is running.
            </div>
          </div>
        )}
        
        {/* Main Content Area */}
        <div className="management-main">
          {/* Create Template */}
          <ManagementPanel 
            title="Create New Template"
            icon="➕"
            subtitle={error ? "Backend unavailable - see error message above" : "Add a new template with extended type support"}
            position="primary"
          >
            <form onSubmit={handleCreateTemplate}>
              <div style={{ opacity: error ? 0.5 : 1, pointerEvents: error ? 'none' : 'auto' }}>
              {/* Basic Information Section */}
              <div className="form-section">
                <h3 className="form-section-title">Basic Information</h3>
                <ContentGrid columns={2} gap="md">
                  <div className="form-field">
                    <label htmlFor="template-type">Template Type</label>
                    <select
                      id="template-type"
                      value={createType}
                      onChange={(e) => setCreateType(e.target.value)}
                      disabled={isCreating}
                      required
                    >
                      <option value={JobType}>Job Template</option>
                      <option value={ConfigType}>Configuration</option>
                      <option value={ComplianceType}>Compliance</option>
                    </select>
                  </div>
                  <div className="form-field">
                    <label htmlFor="template-name">Template Name</label>
                    <input
                      id="template-name"
                      type="text"
                      value={createName}
                      onChange={(e) => setCreateName(e.target.value)}
                      placeholder="e.g. Ubuntu Provision"
                      disabled={isCreating}
                      required
                    />
                  </div>
                  <div className="form-field">
                    <label htmlFor="template-provider">Provider</label>
                    <input
                      id="template-provider"
                      type="text"
                      value={createProvider}
                      onChange={(e) => setCreateProvider(e.target.value)}
                      placeholder="e.g. ansible, terraform"
                      disabled={isCreating}
                      required
                    />
                  </div>
                  <div className="form-field">
                    <label htmlFor="template-description">Description</label>
                    <input
                      id="template-description"
                      type="text"
                      value={createDescription}
                      onChange={(e) => setCreateDescription(e.target.value)}
                      placeholder="Template description"
                      disabled={isCreating}
                    />
                  </div>
                </ContentGrid>
              </div>

              {/* Type-specific Configuration */}
              {createType === JobType && (
                <div className="form-section">
                  <h3 className="form-section-title">Job Configuration</h3>
                  <ContentGrid columns={2} gap="md">
                    <div className="form-field">
                      <label htmlFor="job-type">Job Type</label>
                      <input
                        id="job-type"
                        type="text"
                        value={createJobType}
                        onChange={(e) => setCreateJobType(e.target.value)}
                        placeholder="e.g. provision, configure, cleanup"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="execution-mode">Execution Mode</label>
                      <select
                        id="execution-mode"
                        value={createExecutionMode}
                        onChange={(e) => setCreateExecutionMode(e.target.value as 'sequential' | 'parallel')}
                        disabled={isCreating}
                      >
                        <option value="sequential">Sequential</option>
                        <option value="parallel">Parallel</option>
                      </select>
                    </div>
                    <div className="form-field">
                      <label htmlFor="timeout">Timeout (seconds)</label>
                      <input
                        id="timeout"
                        type="number"
                        value={createTimeout}
                        onChange={(e) => setCreateTimeout(e.target.value)}
                        placeholder="3600"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="max-retries">Max Retries</label>
                      <input
                        id="max-retries"
                        type="number"
                        value={createMaxRetries}
                        onChange={(e) => setCreateMaxRetries(e.target.value)}
                        placeholder="3"
                        disabled={isCreating}
                      />
                    </div>
                  </ContentGrid>
                  <ContentGrid columns={1} gap="md">
                    <div className="form-field">
                      <label htmlFor="environment">Environment Variables (JSON)</label>
                      <textarea
                        id="environment"
                        value={createEnvironment}
                        onChange={(e) => setCreateEnvironment(e.target.value)}
                        placeholder='{"NODE_ENV": "production", "API_URL": "https://api.example.com"}'
                        disabled={isCreating}
                        rows={3}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="requirements">Requirements</label>
                      <textarea
                        id="requirements"
                        value={createRequirements}
                        onChange={(e) => setCreateRequirements(e.target.value)}
                        placeholder="List any special requirements or dependencies"
                        disabled={isCreating}
                        rows={2}
                      />
                    </div>
                  </ContentGrid>
                  
                  {/* Playbook Upload */}
                  <ContentGrid columns={1} gap="md">
                    <div className="form-field">
                      <label htmlFor="playbook-upload">Playbook File</label>
                      <div className="file-upload-area">
                        <input
                          id="playbook-upload"
                          type="file"
                          accept=".yml,.yaml,.playbook"
                          onChange={handlePlaybookUpload}
                          disabled={isCreating || isUploading}
                          className="file-input"
                        />
                        <div className="file-upload-label">
                          <div className="file-upload-icon">📁</div>
                          <div className="file-upload-text">
                            <p>Click to upload or drag and drop</p>
                            <small>YAML, YML, PLAYBOOK files (MAX. 10MB)</small>
                          </div>
                        </div>
                        {playbookFile && (
                          <div className="file-upload-success">
                            <span className="file-name">{playbookFile.name}</span>
                            <button
                              type="button"
                              className="ghost-button small"
                              onClick={() => setPlaybookFile(null)}
                              disabled={isCreating}
                            >
                              Remove
                            </button>
                          </div>
                        )}
                        {isUploading && uploadProgress > 0 && (
                          <div className="upload-progress">
                            <div className="progress-bar" style={{ width: `${uploadProgress}%` }}></div>
                            <span>{uploadProgress}%</span>
                          </div>
                        )}
                      </div>
                    </div>
                  </ContentGrid>
                </div>
              )}

              {createType === ConfigType && (
                <>
                  <ContentGrid columns={2} gap="md">
                    <div className="form-field">
                      <label htmlFor="config-type">Config Type</label>
                      <select
                        id="config-type"
                        value={createConfigType}
                        onChange={(e) => setCreateConfigType(e.target.value as 'tenant' | 'node' | 'global')}
                        disabled={isCreating}
                      >
                        <option value="node">Node Configuration</option>
                        <option value="tenant">Tenant Configuration</option>
                        <option value="global">Global Configuration</option>
                      </select>
                    </div>
                    <div className="form-field">
                      <label htmlFor="default-values">Default Values (JSON)</label>
                      <textarea
                        id="default-values"
                        value={createDefaultValues}
                        onChange={(e) => setCreateDefaultValues(e.target.value)}
                        placeholder='{"key": "value"}'
                        disabled={isCreating}
                        rows={3}
                      />
                    </div>
                  </ContentGrid>
                  
                  {/* Terraform Upload */}
                  <ContentGrid columns={1} gap="md">
                    <div className="form-field">
                      <label htmlFor="terraform-upload">Terraform Configuration</label>
                      <div className="file-upload-area">
                        <input
                          id="terraform-upload"
                          type="file"
                          accept=".tf,.tf.json"
                          onChange={handleTerraformUpload}
                          disabled={isCreating || isUploading}
                          className="file-input"
                        />
                        <div className="file-upload-label">
                          <div className="file-upload-icon">🏗️</div>
                          <div className="file-upload-text">
                            <p>Click to upload or drag and drop</p>
                            <small>Terraform (.tf, .tf.json) files (MAX. 10MB)</small>
                          </div>
                        </div>
                        {terraformFile && (
                          <div className="file-upload-success">
                            <span className="file-name">{terraformFile.name}</span>
                            <button
                              type="button"
                              className="ghost-button small"
                              onClick={() => setTerraformFile(null)}
                              disabled={isCreating}
                            >
                              Remove
                            </button>
                          </div>
                        )}
                        {isUploading && uploadProgress > 0 && (
                          <div className="upload-progress">
                            <div className="progress-bar" style={{ width: `${uploadProgress}%` }}></div>
                            <span>{uploadProgress}%</span>
                          </div>
                        )}
                      </div>
                    </div>
                  </ContentGrid>
                </>
              )}

              {createType === ComplianceType && (
                <>
                  <ContentGrid columns={2} gap="md">
                    <div className="form-field">
                      <label htmlFor="compliance-type">Compliance Type</label>
                      <select
                        id="compliance-type"
                        value={createComplianceType}
                        onChange={(e) => setCreateComplianceType(e.target.value as 'scan' | 'remediation' | 'policy')}
                        disabled={isCreating}
                      >
                        <option value="scan">Security Scan</option>
                        <option value="remediation">Remediation</option>
                        <option value="policy">Policy Enforcement</option>
                      </select>
                    </div>
                    <div className="form-field">
                      <label htmlFor="rule-set">Rule Set</label>
                      <input
                        id="rule-set"
                        type="text"
                        value={createRuleSet}
                        onChange={(e) => setCreateRuleSet(e.target.value)}
                        placeholder="e.g. CIS, NIST, custom"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="compliance-framework">Compliance Framework</label>
                      <input
                        id="compliance-framework"
                        type="text"
                        value={createComplianceFramework}
                        onChange={(e) => setCreateComplianceFramework(e.target.value)}
                        placeholder="e.g. SOC2, HIPAA, GDPR, PCI-DSS"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="severity-levels">Severity Levels</label>
                      <input
                        id="severity-levels"
                        type="text"
                        value={createSeverityLevels}
                        onChange={(e) => setCreateSeverityLevels(e.target.value)}
                        placeholder="e.g. low,medium,high,critical"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="schedule">Schedule (Cron)</label>
                      <input
                        id="schedule"
                        type="text"
                        value={createSchedule}
                        onChange={(e) => setCreateSchedule(e.target.value)}
                        placeholder="e.g. 0 2 * * * (daily at 2 AM)"
                        disabled={isCreating}
                      />
                    </div>
                    <div className="form-field">
                      <label htmlFor="notification-threshold">Notification Threshold</label>
                      <select
                        id="notification-threshold"
                        value={createNotificationThreshold}
                        onChange={(e) => setCreateNotificationThreshold(e.target.value)}
                        disabled={isCreating}
                      >
                        <option value="">Select threshold</option>
                        <option value="low">Low</option>
                        <option value="medium">Medium</option>
                        <option value="high">High</option>
                        <option value="critical">Critical</option>
                      </select>
                    </div>
                  </ContentGrid>
                  
                  <ContentGrid columns={1} gap="md">
                    <div className="form-field checkbox-inline">
                      <label>
                        <input
                          type="checkbox"
                          checked={createRemediationAvailable}
                          onChange={(e) => setCreateRemediationAvailable(e.target.checked)}
                          disabled={isCreating}
                        />
                        Remediation Available
                        <small>Enable automatic remediation for failed compliance checks</small>
                      </label>
                    </div>
                  </ContentGrid>
                </>
              )}

              {formError && <div className="form-error">{formError}</div>}
              {formSuccess && <div className="form-success">{formSuccess}</div>}
              
              <ActionZone alignment="right" variant="primary">
                <button type="submit" className="primary-button" disabled={isCreating}>
                  {isCreating ? 'Creating…' : 'Create Template'}
                </button>
              </ActionZone>
            </div>
            </form>
          </ManagementPanel>

          {/* Template List */}
          <ManagementPanel 
            title="📚 Template Library"
            subtitle="Browse and manage all available job templates"
            position="primary"
          >
            {loading ? (
              <p className="muted">Loading templates…</p>
            ) : filteredTemplates.length === 0 ? (
              <div className="empty-state">
                <p>No templates found. Create your first template to get started.</p>
              </div>
            ) : (
              <div className="template-list">
                {filteredTemplates.map((template) => (
                  <div key={template.id} className="template-card">
                    <header>
                      <div className="template-header-info">
                        <span className="template-icon">{getTemplateIcon(template)}</span>
                        <h3>{template.name}</h3>
                        <div className="template-badges">
                          <span className="type-badge">{getTemplateTypeLabel(template.type)}</span>
                          <span className={`status-pill status-${getTemplateStatus(template)}`}>
                            {getTemplateStatus(template)}
                          </span>
                        </div>
                      </div>
                    </header>
                    <dl>
                      <dt>Provider</dt>
                      <dd>{template.provider}</dd>
                      <dt>Description</dt>
                      <dd>{template.description || '—'}</dd>
                      <dt>Created</dt>
                      <dd>{formatDate(template.created_at)}</dd>
                      <dt>Updated</dt>
                      <dd>{formatDate(template.updated_at)}</dd>
                    </dl>
                    <ActionZone alignment="right" variant="secondary">
                      <button
                        type="button"
                        className="ghost-button"
                        onClick={() => setSelectedTemplateId(template.id)}
                      >
                        Manage
                      </button>
                    </ActionZone>
                  </div>
                ))}
              </div>
            )}
          </ManagementPanel>
        </div>

        {/* Sidebar */}
        <div className="management-sidebar">
          {/* Filters */}
          <ManagementPanel 
            title="Filters"
            icon="🔍"
            subtitle={`${filteredTemplates.length} templates shown`}
            position="secondary"
          >
            <ContentGrid columns={1} gap="md">
              <div className="form-field">
                <label htmlFor="type-filter">Type</label>
                <select
                  id="type-filter"
                  value={typeFilter}
                  onChange={(e) => setTypeFilter(e.target.value as TemplateType | 'all')}
                >
                  <option value="all">All Types</option>
                  <option value="job">Job Templates</option>
                  <option value="config">Configuration</option>
                  <option value="compliance">Compliance</option>
                </select>
              </div>
              <div className="form-field">
                <label htmlFor="provider-filter">Provider</label>
                <select
                  id="provider-filter"
                  value={providerFilter}
                  onChange={(e) => setProviderFilter(e.target.value)}
                >
                  <option value="">All Providers</option>
                  {availableProviders.map(provider => (
                    <option key={provider} value={provider}>{provider}</option>
                  ))}
                </select>
              </div>
              <div className="form-field">
                <label htmlFor="name-filter">Name</label>
                <input
                  id="name-filter"
                  type="text"
                  value={nameFilter}
                  onChange={(e) => setNameFilter(e.target.value)}
                  placeholder="e.g. Ubuntu"
                />
              </div>
              <div className="form-field checkbox-inline">
                <label>
                  <input
                    type="checkbox"
                    checked={includeArchived}
                    onChange={(e) => setIncludeArchived(e.target.checked)}
                  />
                  Include archived templates
                </label>
              </div>
            </ContentGrid>
          </ManagementPanel>

          {/* Template Management */}
          {selectedTemplate && (
            <ManagementPanel 
              title="Template Management"
              icon="🔧"
              subtitle={`Provider: ${selectedTemplate.provider}`}
              position="secondary"
            >
              <form onSubmit={handleUpdateTemplate}>
                <ContentGrid columns={1} gap="md">
                  <div className="form-field">
                    <label htmlFor="update-name">Template Name</label>
                    <input
                      id="update-name"
                      type="text"
                      value={updateName || selectedTemplate.name}
                      onChange={(e) => setUpdateName(e.target.value)}
                      disabled={isUpdating}
                    />
                  </div>
                  <div className="form-field">
                    <label htmlFor="update-description">Description</label>
                    <input
                      id="update-description"
                      type="text"
                      value={updateDescription || selectedTemplate.description || ''}
                      onChange={(e) => setUpdateDescription(e.target.value)}
                      disabled={isUpdating}
                    />
                  </div>
                </ContentGrid>
                
                <ActionZone alignment="right" variant="primary">
                  <button type="submit" className="primary-button" disabled={isUpdating}>
                    {isUpdating ? 'Updating…' : 'Update Template'}
                  </button>
                  {selectedTemplate?.archived_at ? (
                    <button
                      type="button"
                      className="primary-button"
                      onClick={handleUnarchiveTemplate}
                      disabled={isUnarchiving}
                    >
                      {isUnarchiving ? 'Unarchiving…' : 'Unarchive'}
                    </button>
                  ) : (
                    <button
                      type="button"
                      className="danger-button"
                      onClick={handleArchiveTemplate}
                      disabled={isArchiving}
                    >
                      {isArchiving ? 'Archiving…' : 'Archive'}
                    </button>
                  )}
                  <button
                    type="button"
                    className="ghost-button"
                    onClick={() => setSelectedTemplateId(null)}
                  >
                    Close
                  </button>
                </ActionZone>
              </form>
            </ManagementPanel>
          )}
        </div>
      </div>

      {/* Pagination */}
      {pagination.total > pagination.limit && (
        <ManagementPanel 
          title="Pagination"
          position="tertiary"
        >
          <div className="pagination-controls">
            <div className="pagination-info">
              Showing {pagination.offset + 1}-{Math.min(pagination.offset + pagination.limit, pagination.total)} of {pagination.total} templates
            </div>
            <ActionZone alignment="center" variant="secondary">
              <button
                className="ghost-button"
                disabled={pagination.offset === 0}
                onClick={() => setOffset(Math.max(0, pagination.offset - pagination.limit))}
              >
                Previous
              </button>
              <button
                className="ghost-button"
                disabled={!pagination.hasMore}
                onClick={() => setOffset(pagination.offset + pagination.limit)}
              >
                Next
              </button>
            </ActionZone>
          </div>
        </ManagementPanel>
      )}
    </EnterpriseLayout>
  );
}
