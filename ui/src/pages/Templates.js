import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useExtendedTemplates } from '../hooks/useExtendedTemplates';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { DemandForm } from '../components/DemandForm';
import { summarizeTemplates, filterTemplates, getTemplateProviders, getTemplateIcon, getTemplateTypeLabel, getTemplateStatus } from '../lib/extendedTemplateUtils';
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
    const { data: extendedTemplates, loading, reload } = useExtendedTemplates();
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
    // Template versions for selected template
    const { data: versions } = useTemplateVersions(selectedTemplateId ? { templateId: selectedTemplateId } : {});
    const [isCreating, setIsCreating] = useState(false);
    const [isUpdating, setIsUpdating] = useState(false);
    const [isArchiving, setIsArchiving] = useState(false);
    const [isUnarchiving, setIsUnarchiving] = useState(false);
    // Form state
    const [createName, setCreateName] = useState('');
    const [createProvider, setCreateProvider] = useState('');
    const [createDescription, setCreateDescription] = useState('');
    const [createLabels, setCreateLabels] = useState('');
    const [updateName, setUpdateName] = useState(selectedTemplate?.name ?? '');
    const [updateDescription, setUpdateDescription] = useState(selectedTemplate?.description ?? '');
    const [updateLabels, setUpdateLabels] = useState(selectedTemplate?.labels ? JSON.stringify(selectedTemplate.labels) : '');
    const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();
    const handleCreateTemplate = async (event) => {
        event.preventDefault();
        const trimmedName = createName.trim();
        const trimmedProvider = createProvider.trim();
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
            await api.createTemplate({
                name: trimmedName,
                provider: trimmedProvider,
            });
            showSuccess(`Template "${trimmedName}" created successfully.`);
            showToast('Template created successfully.', 'success');
            setCreateName('');
            setCreateProvider('');
            setCreateDescription('');
            setCreateLabels('');
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
    return (_jsxs("div", { className: "focused-content", children: [_jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCCB Template Management" }), _jsx("p", { className: "focused-section-subtitle", children: "Manage job templates for provisioning and compliance workflows." })] }), _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "stat-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total Templates" }), _jsx("strong", { children: summary.total }), _jsx("small", { className: "muted", children: "All templates" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Active" }), _jsx("strong", { children: summary.active }), _jsx("small", { className: "muted", children: "Available for use" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Providers" }), _jsx("strong", { children: summary.providers }), _jsx("small", { className: "muted", children: "Integration types" })] })] }) })] }), _jsx(DemandForm, { title: "Create New Template", icon: "\u2795", summary: "Add a new job template", children: _jsxs("form", { onSubmit: handleCreateTemplate, children: [_jsxs("div", { className: "compact-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-name", children: "Template Name" }), _jsx("input", { id: "template-name", type: "text", value: createName, onChange: (e) => setCreateName(e.target.value), placeholder: "e.g. Ubuntu Provision", disabled: isCreating, required: true })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-provider", children: "Provider" }), _jsx("input", { id: "template-provider", type: "text", value: createProvider, onChange: (e) => setCreateProvider(e.target.value), placeholder: "e.g. ansible, terraform", disabled: isCreating, required: true })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-description", children: "Description" }), _jsx("input", { id: "template-description", type: "text", value: createDescription, onChange: (e) => setCreateDescription(e.target.value), placeholder: "Template description", disabled: isCreating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "template-labels", children: "Labels" }), _jsx("input", { id: "template-labels", type: "text", value: createLabels, onChange: (e) => setCreateLabels(e.target.value), placeholder: "key1=value1, key2=value2", disabled: isCreating })] })] }), formError && _jsx("div", { className: "form-error", children: formError }), formSuccess && _jsx("div", { className: "form-success", children: formSuccess }), _jsx("button", { type: "submit", className: "primary-button", disabled: isCreating, children: isCreating ? 'Creating…' : 'Create Template' })] }) }), _jsx(DemandForm, { title: "Filters", icon: "\uD83D\uDD0D", summary: `${filteredTemplates.length} templates shown`, children: _jsxs("div", { className: "compact-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "type-filter", children: "Type" }), _jsxs("select", { id: "type-filter", value: typeFilter, onChange: (e) => setTypeFilter(e.target.value), children: [_jsx("option", { value: "all", children: "All Types" }), _jsx("option", { value: "job", children: "Job Templates" }), _jsx("option", { value: "config", children: "Configuration" }), _jsx("option", { value: "compliance", children: "Compliance" })] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "provider-filter", children: "Provider" }), _jsxs("select", { id: "provider-filter", value: providerFilter, onChange: (e) => setProviderFilter(e.target.value), children: [_jsx("option", { value: "", children: "All Providers" }), availableProviders.map(provider => (_jsx("option", { value: provider, children: provider }, provider)))] })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "name-filter", children: "Name" }), _jsx("input", { id: "name-filter", type: "text", value: nameFilter, onChange: (e) => setNameFilter(e.target.value), placeholder: "e.g. Ubuntu" })] }), _jsx("div", { className: "form-field", children: _jsxs("label", { children: [_jsx("input", { type: "checkbox", checked: includeArchived, onChange: (e) => setIncludeArchived(e.target.checked) }), "Include archived templates"] }) })] }) }), _jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCDA Template Library" }), _jsx("p", { className: "focused-section-subtitle", children: "Browse and manage all available job templates" })] }), _jsx("div", { className: "focused-section-content", children: loading ? (_jsx("p", { className: "muted", children: "Loading templates\u2026" })) : filteredTemplates.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No templates found. Create your first template to get started." }) })) : (_jsx("div", { className: "template-list", children: filteredTemplates.map((template) => (_jsxs("div", { className: "template-card", children: [_jsx("header", { children: _jsxs("div", { className: "template-header-info", children: [_jsx("span", { className: "template-icon", children: getTemplateIcon(template) }), _jsx("h3", { children: template.name }), _jsxs("div", { className: "template-badges", children: [_jsx("span", { className: "type-badge", children: getTemplateTypeLabel(template.type) }), _jsx("span", { className: `status-pill status-${getTemplateStatus(template)}`, children: getTemplateStatus(template) })] })] }) }), _jsxs("dl", { children: [_jsx("dt", { children: "Provider" }), _jsx("dd", { children: template.provider }), _jsx("dt", { children: "Description" }), _jsx("dd", { children: template.description || '—' }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(template.created_at) }), _jsx("dt", { children: "Updated" }), _jsx("dd", { children: formatDate(template.updated_at) })] }), _jsx("div", { className: "template-actions", children: _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTemplateId(template.id), children: "Manage" }) })] }, template.id))) })) })] }), pagination.total > pagination.limit && (_jsx("div", { className: "focused-section", children: _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "pagination-controls", children: [_jsxs("div", { className: "pagination-info", children: ["Showing ", pagination.offset + 1, "-", Math.min(pagination.offset + pagination.limit, pagination.total), " of ", pagination.total, " templates"] }), _jsxs("div", { className: "pagination-buttons", children: [_jsx("button", { className: "ghost-button", disabled: pagination.offset === 0, onClick: () => setOffset(Math.max(0, pagination.offset - pagination.limit)), children: "Previous" }), _jsx("button", { className: "ghost-button", disabled: !pagination.hasMore, onClick: () => setOffset(pagination.offset + pagination.limit), children: "Next" })] })] }) }) })), selectedTemplate && (_jsx(DemandForm, { title: "Template Management", icon: "\uD83D\uDD27", summary: `Provider: ${selectedTemplate.provider}`, defaultExpanded: true, children: _jsx("div", { className: "template-management", children: _jsxs("form", { onSubmit: handleUpdateTemplate, children: [_jsxs("div", { className: "compact-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "update-name", children: "Template Name" }), _jsx("input", { id: "update-name", type: "text", value: updateName || selectedTemplate.name, onChange: (e) => setUpdateName(e.target.value), disabled: isUpdating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "update-description", children: "Description" }), _jsx("input", { id: "update-description", type: "text", value: updateDescription || selectedTemplate.description || '', onChange: (e) => setUpdateDescription(e.target.value), disabled: isUpdating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "update-labels", children: "Labels" }), _jsx("input", { id: "update-labels", type: "text", value: updateLabels || (selectedTemplate?.labels ? Object.entries(selectedTemplate.labels).map(([k, v]) => `${k}=${v}`).join(', ') : ''), onChange: (e) => setUpdateLabels(e.target.value), disabled: isUpdating })] })] }), selectedTemplate && versions && versions.length > 0 && (_jsxs("div", { className: "template-versions", children: [_jsxs("h4", { children: ["Template Versions (", versions.length, ")"] }), _jsx("div", { className: "version-list", children: versions.map((version) => (_jsxs("div", { className: "version-card", children: [_jsxs("div", { className: "version-info", children: [_jsxs("span", { className: "version-number", children: ["v", version.version] }), _jsx("span", { className: "version-date", children: formatDate(version.created_at) }), version.promoted_at && (_jsx("span", { className: "promoted-badge", children: "Promoted" }))] }), version.rollout_notes && (_jsxs("div", { className: "version-notes", children: [_jsx("strong", { children: "Notes:" }), " ", version.rollout_notes] }))] }, version.id))) })] })), _jsxs("div", { className: "form-actions", children: [_jsx("button", { type: "submit", className: "primary-button", disabled: isUpdating, children: isUpdating ? 'Updating…' : 'Update Template' }), selectedTemplate?.archived_at ? (_jsx("button", { type: "button", className: "primary-button", onClick: handleUnarchiveTemplate, disabled: isUnarchiving, children: isUnarchiving ? 'Unarchiving…' : 'Unarchive' })) : (_jsx("button", { type: "button", className: "danger-button", onClick: handleArchiveTemplate, disabled: isArchiving, children: isArchiving ? 'Archiving…' : 'Archive' })), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTemplateId(null), children: "Close" })] })] }) }) }))] }));
}
