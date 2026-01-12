import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useExtendedTemplates } from '../hooks/useExtendedTemplates';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { EnterpriseLayout, ExecutiveOverview, ManagementPanel, ActionZone, ContentGrid } from '../components/EnterpriseLayout';
import { summarizeTemplates, filterTemplates, getTemplateProviders, getTemplateIcon, getTemplateTypeLabel, getTemplateStatus } from '../lib/extendedTemplateUtils';
import '../components/EnterpriseLayout.css';
// Import the enum values for runtime use
const JobType = 'job';
const ConfigType = 'config';
const ComplianceType = 'compliance';
import { parseTemplateLabels, } from '../lib/templateUtils';
function formatDate(value) {
    if (!value) {
        return '—';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return value;
    }
    return date.toLocaleString();
}
export function Templates() {
    const api = useApiClient();
    const { showToast } = useToast();
    const [limit] = useState(20);
    const [offset, setOffset] = useState(0);
    const [providerFilter, setProviderFilter] = useState('');
    const [nameFilter, setNameFilter] = useState('');
    const [typeFilter, setTypeFilter] = useState('all');
    const [includeArchived, setIncludeArchived] = useState(false);
    // Use extended templates system
    const { data: extendedTemplates, loading, error, reload } = useExtendedTemplates();
    // Filter templates
    const filter = {
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
    const [selectedTemplateId, setSelectedTemplateId] = useState(null);
    useEffect(() => {
        if (filteredTemplates.length === 0) {
            setSelectedTemplateId(null);
            return;
        }
        if (!selectedTemplateId && filteredTemplates.length > 0) {
            setSelectedTemplateId(filteredTemplates[0].id);
        }
    }, [filteredTemplates, selectedTemplateId]);
    const selectedTemplate = useMemo(() => extendedTemplates.find((t) => t.id === selectedTemplateId) ?? null, [extendedTemplates, selectedTemplateId]);
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
    const [createType, setCreateType] = useState(JobType);
    // Job template specific fields
    const [createJobType, setCreateJobType] = useState('');
    const [createTimeout, setCreateTimeout] = useState('');
    const [createMaxRetries, setCreateMaxRetries] = useState('');
    const [createRetryDelay, setCreateRetryDelay] = useState('');
    const [createExecutionMode, setCreateExecutionMode] = useState('sequential');
    const [createEnvironment, setCreateEnvironment] = useState('');
    const [createRequirements, setCreateRequirements] = useState('');
    // Config template specific fields
    const [createConfigType, setCreateConfigType] = useState('node');
    const [createDefaultValues, setCreateDefaultValues] = useState('');
    // Compliance template specific fields
    const [createComplianceType, setCreateComplianceType] = useState('scan');
    const [createRuleSet, setCreateRuleSet] = useState('');
    const [createSeverityLevels, setCreateSeverityLevels] = useState('');
    const [createRemediationAvailable, setCreateRemediationAvailable] = useState(false);
    const [createComplianceFramework, setCreateComplianceFramework] = useState('');
    const [createSchedule, setCreateSchedule] = useState('');
    const [createNotificationThreshold, setCreateNotificationThreshold] = useState('');
    // File upload states
    const [playbookFile, setPlaybookFile] = useState(null);
    const [terraformFile, setTerraformFile] = useState(null);
    const [uploadProgress, setUploadProgress] = useState(0);
    const [isUploading, setIsUploading] = useState(false);
    const [updateName, setUpdateName] = useState(selectedTemplate?.name ?? '');
    const [updateDescription, setUpdateDescription] = useState(selectedTemplate?.description ?? '');
    const [updateLabels] = useState(selectedTemplate?.labels ? JSON.stringify(selectedTemplate.labels) : '');
    const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();
    const [isCreating, setIsCreating] = useState(false);
    const [isUpdating, setIsUpdating] = useState(false);
    const [isArchiving, setIsArchiving] = useState(false);
    const [isUnarchiving, setIsUnarchiving] = useState(false);
    // File upload handlers
    const handleFileUpload = async (file, type) => {
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
            }
            else {
                setTerraformFile(file);
            }
            showSuccess(`${type === 'playbook' ? 'Playbook' : 'Terraform file'} uploaded successfully.`);
        }
        catch (err) {
            const message = err instanceof Error ? err.message : `Failed to upload ${type}.`;
            showError(message);
        }
        finally {
            setIsUploading(false);
            setTimeout(() => setUploadProgress(0), 1000);
        }
    };
    const handlePlaybookUpload = (event) => {
        const file = event.target.files?.[0];
        if (file) {
            if (file.name.endsWith('.yml') || file.name.endsWith('.yaml') || file.name.endsWith('.playbook')) {
                handleFileUpload(file, 'playbook');
            }
            else {
                showError('Please upload a valid playbook file (.yml, .yaml, .playbook)');
            }
        }
    };
    const handleTerraformUpload = (event) => {
        const file = event.target.files?.[0];
        if (file) {
            if (file.name.endsWith('.tf') || file.name.endsWith('.tf.json')) {
                handleFileUpload(file, 'terraform');
            }
            else {
                showError('Please upload a valid Terraform file (.tf, .tf.json)');
            }
        }
    };
    const handleCreateTemplate = async (event) => {
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
                    template_type: 'job',
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
            }
            else if (createType === ConfigType) {
                payload = {
                    ...basePayload,
                    template_type: 'config',
                    labels: {
                        ...(labels || {}),
                        template_type: 'config',
                        config_type: createConfigType,
                        ...(createDefaultValues.trim() && { default_values: createDefaultValues }),
                        ...(terraformFile && { terraform_file: terraformFile.name }),
                    },
                };
            }
            else if (createType === ComplianceType) {
                payload = {
                    ...basePayload,
                    template_type: 'compliance',
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
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to create template.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setIsCreating(false);
        }
    };
    const handleUpdateTemplate = async (event) => {
        event.preventDefault();
        if (!selectedTemplate)
            return;
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
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to update template.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setIsUpdating(false);
        }
    };
    const handleArchiveTemplate = async () => {
        if (!selectedTemplate)
            return;
        const confirmed = window.confirm(`Archive template "${selectedTemplate.name}"? This will hide it from the template list.`);
        if (!confirmed)
            return;
        setIsArchiving(true);
        try {
            await api.archiveTemplate(selectedTemplate.id);
            showSuccess('Template archived successfully.');
            showToast('Template archived successfully.', 'success');
            reload();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to archive template.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setIsArchiving(false);
        }
    };
    const handleUnarchiveTemplate = async () => {
        if (!selectedTemplate)
            return;
        setIsUnarchiving(true);
        try {
            await api.unarchiveTemplate(selectedTemplate.id);
            showSuccess('Template unarchived successfully.');
            showToast('Template unarchived successfully.', 'success');
            reload();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to unarchive template.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setIsUnarchiving(false);
        }
    };
    return (_jsxs(EnterpriseLayout, { variant: "management", children: [_jsxs(ExecutiveOverview, { title: "\uD83D\uDCCB Template Management", subtitle: "Manage job templates for provisioning and compliance workflows", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total Templates" }), _jsx("strong", { children: summary.total }), _jsx("small", { className: "muted", children: "All templates" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Active" }), _jsx("strong", { children: summary.active }), _jsx("small", { className: "muted", children: "Available for use" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Providers" }), _jsx("strong", { children: summary.providers }), _jsx("small", { className: "muted", children: "Integration types" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Compliance" }), _jsx("strong", { children: (extendedTemplates.filter(t => t.type === 'compliance')).length }), _jsx("small", { className: "muted", children: "Security templates" })] })] }), _jsxs("div", { className: "management-dashboard", children: [error && (_jsxs("div", { className: "form-error", style: { marginBottom: '2rem', padding: '1rem' }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }, children: [_jsx("span", { style: { fontSize: '1.2rem' }, children: "\u26A0\uFE0F" }), _jsx("strong", { children: "Backend Connection Error" })] }), _jsx("div", { style: { fontSize: '0.9rem', lineHeight: '1.4' }, children: error }), _jsx("div", { style: { fontSize: '0.8rem', marginTop: '0.5rem', opacity: '0.8' }, children: "Template creation and management will be unavailable until the backend is running." })] })), _jsxs("div", { className: "management-main", children: [_jsx(ManagementPanel, { title: "Create New Template", icon: "\u2795", subtitle: error ? "Backend unavailable - see error message above" : "Add a new template with extended type support", position: "primary", children: _jsx("form", { onSubmit: handleCreateTemplate, children: _jsxs("div", { style: { opacity: error ? 0.5 : 1, pointerEvents: error ? 'none' : 'auto' }, children: [_jsxs("div", { className: "form-section", children: [_jsx("h3", { className: "form-section-title", children: "Basic Information" }), _jsxs(ContentGrid, { columns: 2, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-type", children: "Template Type" }), _jsxs("select", { id: "template-type", value: createType, onChange: (e) => setCreateType(e.target.value), disabled: isCreating, required: true, children: [_jsx("option", { value: JobType, children: "Job Template" }), _jsx("option", { value: ConfigType, children: "Configuration" }), _jsx("option", { value: ComplianceType, children: "Compliance" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-name", children: "Template Name" }), _jsx("input", { id: "template-name", type: "text", value: createName, onChange: (e) => setCreateName(e.target.value), placeholder: "e.g. Ubuntu Provision", disabled: isCreating, required: true })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-provider", children: "Provider" }), _jsx("input", { id: "template-provider", type: "text", value: createProvider, onChange: (e) => setCreateProvider(e.target.value), placeholder: "e.g. ansible, terraform", disabled: isCreating, required: true })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-description", children: "Description" }), _jsx("input", { id: "template-description", type: "text", value: createDescription, onChange: (e) => setCreateDescription(e.target.value), placeholder: "Template description", disabled: isCreating })] })] })] }), createType === JobType && (_jsxs("div", { className: "form-section", children: [_jsx("h3", { className: "form-section-title", children: "Job Configuration" }), _jsxs(ContentGrid, { columns: 2, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "job-type", children: "Job Type" }), _jsx("input", { id: "job-type", type: "text", value: createJobType, onChange: (e) => setCreateJobType(e.target.value), placeholder: "e.g. provision, configure, cleanup", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "execution-mode", children: "Execution Mode" }), _jsxs("select", { id: "execution-mode", value: createExecutionMode, onChange: (e) => setCreateExecutionMode(e.target.value), disabled: isCreating, children: [_jsx("option", { value: "sequential", children: "Sequential" }), _jsx("option", { value: "parallel", children: "Parallel" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "timeout", children: "Timeout (seconds)" }), _jsx("input", { id: "timeout", type: "number", value: createTimeout, onChange: (e) => setCreateTimeout(e.target.value), placeholder: "3600", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "max-retries", children: "Max Retries" }), _jsx("input", { id: "max-retries", type: "number", value: createMaxRetries, onChange: (e) => setCreateMaxRetries(e.target.value), placeholder: "3", disabled: isCreating })] })] }), _jsxs(ContentGrid, { columns: 1, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "environment", children: "Environment Variables (JSON)" }), _jsx("textarea", { id: "environment", value: createEnvironment, onChange: (e) => setCreateEnvironment(e.target.value), placeholder: '{"NODE_ENV": "production", "API_URL": "https://api.example.com"}', disabled: isCreating, rows: 3 })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "requirements", children: "Requirements" }), _jsx("textarea", { id: "requirements", value: createRequirements, onChange: (e) => setCreateRequirements(e.target.value), placeholder: "List any special requirements or dependencies", disabled: isCreating, rows: 2 })] })] }), _jsx(ContentGrid, { columns: 1, gap: "md", children: _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "playbook-upload", children: "Playbook File" }), _jsxs("div", { className: "file-upload-area", children: [_jsx("input", { id: "playbook-upload", type: "file", accept: ".yml,.yaml,.playbook", onChange: handlePlaybookUpload, disabled: isCreating || isUploading, className: "file-input" }), _jsxs("div", { className: "file-upload-label", children: [_jsx("div", { className: "file-upload-icon", children: "\uD83D\uDCC1" }), _jsxs("div", { className: "file-upload-text", children: [_jsx("p", { children: "Click to upload or drag and drop" }), _jsx("small", { children: "YAML, YML, PLAYBOOK files (MAX. 10MB)" })] })] }), playbookFile && (_jsxs("div", { className: "file-upload-success", children: [_jsx("span", { className: "file-name", children: playbookFile.name }), _jsx("button", { type: "button", className: "ghost-button small", onClick: () => setPlaybookFile(null), disabled: isCreating, children: "Remove" })] })), isUploading && uploadProgress > 0 && (_jsxs("div", { className: "upload-progress", children: [_jsx("div", { className: "progress-bar", style: { width: `${uploadProgress}%` } }), _jsxs("span", { children: [uploadProgress, "%"] })] }))] })] }) })] })), createType === ConfigType && (_jsxs(_Fragment, { children: [_jsxs(ContentGrid, { columns: 2, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "config-type", children: "Config Type" }), _jsxs("select", { id: "config-type", value: createConfigType, onChange: (e) => setCreateConfigType(e.target.value), disabled: isCreating, children: [_jsx("option", { value: "node", children: "Node Configuration" }), _jsx("option", { value: "tenant", children: "Tenant Configuration" }), _jsx("option", { value: "global", children: "Global Configuration" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "default-values", children: "Default Values (JSON)" }), _jsx("textarea", { id: "default-values", value: createDefaultValues, onChange: (e) => setCreateDefaultValues(e.target.value), placeholder: '{"key": "value"}', disabled: isCreating, rows: 3 })] })] }), _jsx(ContentGrid, { columns: 1, gap: "md", children: _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "terraform-upload", children: "Terraform Configuration" }), _jsxs("div", { className: "file-upload-area", children: [_jsx("input", { id: "terraform-upload", type: "file", accept: ".tf,.tf.json", onChange: handleTerraformUpload, disabled: isCreating || isUploading, className: "file-input" }), _jsxs("div", { className: "file-upload-label", children: [_jsx("div", { className: "file-upload-icon", children: "\uD83C\uDFD7\uFE0F" }), _jsxs("div", { className: "file-upload-text", children: [_jsx("p", { children: "Click to upload or drag and drop" }), _jsx("small", { children: "Terraform (.tf, .tf.json) files (MAX. 10MB)" })] })] }), terraformFile && (_jsxs("div", { className: "file-upload-success", children: [_jsx("span", { className: "file-name", children: terraformFile.name }), _jsx("button", { type: "button", className: "ghost-button small", onClick: () => setTerraformFile(null), disabled: isCreating, children: "Remove" })] })), isUploading && uploadProgress > 0 && (_jsxs("div", { className: "upload-progress", children: [_jsx("div", { className: "progress-bar", style: { width: `${uploadProgress}%` } }), _jsxs("span", { children: [uploadProgress, "%"] })] }))] })] }) })] })), createType === ComplianceType && (_jsxs(_Fragment, { children: [_jsxs(ContentGrid, { columns: 2, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "compliance-type", children: "Compliance Type" }), _jsxs("select", { id: "compliance-type", value: createComplianceType, onChange: (e) => setCreateComplianceType(e.target.value), disabled: isCreating, children: [_jsx("option", { value: "scan", children: "Security Scan" }), _jsx("option", { value: "remediation", children: "Remediation" }), _jsx("option", { value: "policy", children: "Policy Enforcement" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "rule-set", children: "Rule Set" }), _jsx("input", { id: "rule-set", type: "text", value: createRuleSet, onChange: (e) => setCreateRuleSet(e.target.value), placeholder: "e.g. CIS, NIST, custom", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "compliance-framework", children: "Compliance Framework" }), _jsx("input", { id: "compliance-framework", type: "text", value: createComplianceFramework, onChange: (e) => setCreateComplianceFramework(e.target.value), placeholder: "e.g. SOC2, HIPAA, GDPR, PCI-DSS", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "severity-levels", children: "Severity Levels" }), _jsx("input", { id: "severity-levels", type: "text", value: createSeverityLevels, onChange: (e) => setCreateSeverityLevels(e.target.value), placeholder: "e.g. low,medium,high,critical", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "schedule", children: "Schedule (Cron)" }), _jsx("input", { id: "schedule", type: "text", value: createSchedule, onChange: (e) => setCreateSchedule(e.target.value), placeholder: "e.g. 0 2 * * * (daily at 2 AM)", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "notification-threshold", children: "Notification Threshold" }), _jsxs("select", { id: "notification-threshold", value: createNotificationThreshold, onChange: (e) => setCreateNotificationThreshold(e.target.value), disabled: isCreating, children: [_jsx("option", { value: "", children: "Select threshold" }), _jsx("option", { value: "low", children: "Low" }), _jsx("option", { value: "medium", children: "Medium" }), _jsx("option", { value: "high", children: "High" }), _jsx("option", { value: "critical", children: "Critical" })] })] })] }), _jsx(ContentGrid, { columns: 1, gap: "md", children: _jsx("div", { className: "form-field checkbox-inline", children: _jsxs("label", { children: [_jsx("input", { type: "checkbox", checked: createRemediationAvailable, onChange: (e) => setCreateRemediationAvailable(e.target.checked), disabled: isCreating }), "Remediation Available", _jsx("small", { children: "Enable automatic remediation for failed compliance checks" })] }) }) })] })), formError && _jsx("div", { className: "form-error", children: formError }), formSuccess && _jsx("div", { className: "form-success", children: formSuccess }), _jsx(ActionZone, { alignment: "right", variant: "primary", children: _jsx("button", { type: "submit", className: "primary-button", disabled: isCreating, children: isCreating ? 'Creating…' : 'Create Template' }) })] }) }) }), _jsx(ManagementPanel, { title: "\uD83D\uDCDA Template Library", subtitle: "Browse and manage all available job templates", position: "primary", children: loading ? (_jsx("p", { className: "muted", children: "Loading templates\u2026" })) : filteredTemplates.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No templates found. Create your first template to get started." }) })) : (_jsx("div", { className: "template-list", children: filteredTemplates.map((template) => (_jsxs("div", { className: "template-card", children: [_jsx("header", { children: _jsxs("div", { className: "template-header-info", children: [_jsx("span", { className: "template-icon", children: getTemplateIcon(template) }), _jsx("h3", { children: template.name }), _jsxs("div", { className: "template-badges", children: [_jsx("span", { className: "type-badge", children: getTemplateTypeLabel(template.type) }), _jsx("span", { className: `status-pill status-${getTemplateStatus(template)}`, children: getTemplateStatus(template) })] })] }) }), _jsxs("dl", { children: [_jsx("dt", { children: "Provider" }), _jsx("dd", { children: template.provider }), _jsx("dt", { children: "Description" }), _jsx("dd", { children: template.description || '—' }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(template.created_at) }), _jsx("dt", { children: "Updated" }), _jsx("dd", { children: formatDate(template.updated_at) })] }), _jsx(ActionZone, { alignment: "right", variant: "secondary", children: _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTemplateId(template.id), children: "Manage" }) })] }, template.id))) })) })] }), _jsxs("div", { className: "management-sidebar", children: [_jsx(ManagementPanel, { title: "Filters", icon: "\uD83D\uDD0D", subtitle: `${filteredTemplates.length} templates shown`, position: "secondary", children: _jsxs(ContentGrid, { columns: 1, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "type-filter", children: "Type" }), _jsxs("select", { id: "type-filter", value: typeFilter, onChange: (e) => setTypeFilter(e.target.value), children: [_jsx("option", { value: "all", children: "All Types" }), _jsx("option", { value: "job", children: "Job Templates" }), _jsx("option", { value: "config", children: "Configuration" }), _jsx("option", { value: "compliance", children: "Compliance" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "provider-filter", children: "Provider" }), _jsxs("select", { id: "provider-filter", value: providerFilter, onChange: (e) => setProviderFilter(e.target.value), children: [_jsx("option", { value: "", children: "All Providers" }), availableProviders.map(provider => (_jsx("option", { value: provider, children: provider }, provider)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "name-filter", children: "Name" }), _jsx("input", { id: "name-filter", type: "text", value: nameFilter, onChange: (e) => setNameFilter(e.target.value), placeholder: "e.g. Ubuntu" })] }), _jsx("div", { className: "form-field checkbox-inline", children: _jsxs("label", { children: [_jsx("input", { type: "checkbox", checked: includeArchived, onChange: (e) => setIncludeArchived(e.target.checked) }), "Include archived templates"] }) })] }) }), selectedTemplate && (_jsx(ManagementPanel, { title: "Template Management", icon: "\uD83D\uDD27", subtitle: `Provider: ${selectedTemplate.provider}`, position: "secondary", children: _jsxs("form", { onSubmit: handleUpdateTemplate, children: [_jsxs(ContentGrid, { columns: 1, gap: "md", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "update-name", children: "Template Name" }), _jsx("input", { id: "update-name", type: "text", value: updateName || selectedTemplate.name, onChange: (e) => setUpdateName(e.target.value), disabled: isUpdating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "update-description", children: "Description" }), _jsx("input", { id: "update-description", type: "text", value: updateDescription || selectedTemplate.description || '', onChange: (e) => setUpdateDescription(e.target.value), disabled: isUpdating })] })] }), _jsxs(ActionZone, { alignment: "right", variant: "primary", children: [_jsx("button", { type: "submit", className: "primary-button", disabled: isUpdating, children: isUpdating ? 'Updating…' : 'Update Template' }), selectedTemplate?.archived_at ? (_jsx("button", { type: "button", className: "primary-button", onClick: handleUnarchiveTemplate, disabled: isUnarchiving, children: isUnarchiving ? 'Unarchiving…' : 'Unarchive' })) : (_jsx("button", { type: "button", className: "danger-button", onClick: handleArchiveTemplate, disabled: isArchiving, children: isArchiving ? 'Archiving…' : 'Archive' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTemplateId(null), children: "Close" })] })] }) }))] })] }), pagination.total > pagination.limit && (_jsx(ManagementPanel, { title: "Pagination", position: "tertiary", children: _jsxs("div", { className: "pagination-controls", children: [_jsxs("div", { className: "pagination-info", children: ["Showing ", pagination.offset + 1, "-", Math.min(pagination.offset + pagination.limit, pagination.total), " of ", pagination.total, " templates"] }), _jsxs(ActionZone, { alignment: "center", variant: "secondary", children: [_jsx("button", { className: "ghost-button", disabled: pagination.offset === 0, onClick: () => setOffset(Math.max(0, pagination.offset - pagination.limit)), children: "Previous" }), _jsx("button", { className: "ghost-button", disabled: !pagination.hasMore, onClick: () => setOffset(pagination.offset + pagination.limit), children: "Next" })] })] }) }))] }));
}
