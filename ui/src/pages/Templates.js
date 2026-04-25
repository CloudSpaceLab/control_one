import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useEffect, useMemo, useState } from 'react';
import { useTemplates } from '../hooks/useTemplates';
import { useTemplateVersions } from '../hooks/useTemplateVersions';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { DEFAULT_METADATA_SCHEMA, DEFAULT_TEMPLATE_BODY, parseJsonInput, parseTemplateLabels, templateStatus, } from '../lib/templateUtils';
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
    const [includeArchived, setIncludeArchived] = useState(false);
    const templateOptions = {
        provider: providerFilter.trim() || undefined,
        namePrefix: nameFilter.trim() || undefined,
        includeArchived,
        limit,
        offset,
    };
    const { data: templates, pagination, loading, error, reload } = useTemplates(templateOptions);
    const [selectedTemplateId, setSelectedTemplateId] = useState(null);
    useEffect(() => {
        if (templates.length === 0) {
            setSelectedTemplateId(null);
            return;
        }
        if (!selectedTemplateId || !templates.some((tpl) => tpl.id === selectedTemplateId)) {
            setSelectedTemplateId(templates[0].id);
        }
    }, [templates, selectedTemplateId]);
    const selectedTemplate = useMemo(() => {
        if (!selectedTemplateId) {
            return null;
        }
        return templates.find((tpl) => tpl.id === selectedTemplateId) ?? null;
    }, [templates, selectedTemplateId]);
    const [versionOffset, setVersionOffset] = useState(0);
    const versionLimit = 10;
    useEffect(() => {
        setVersionOffset(0);
    }, [selectedTemplate?.id]);
    const { data: versions, pagination: versionPagination, loading: versionsLoading, error: versionsError, reload: reloadVersions, } = useTemplateVersions({
        templateId: selectedTemplate?.id,
        limit: versionLimit,
        offset: versionOffset,
    });
    const templateForm = useFormFeedback();
    const versionForm = useFormFeedback();
    const [creatingTemplate, setCreatingTemplate] = useState(false);
    const [creatingVersion, setCreatingVersion] = useState(false);
    const [updatingTemplate, setUpdatingTemplate] = useState(false);
    const [templateName, setTemplateName] = useState('');
    const [templateProvider, setTemplateProvider] = useState('');
    const [templateDescription, setTemplateDescription] = useState('');
    const [templateLabels, setTemplateLabels] = useState('{}');
    const [versionBody, setVersionBody] = useState(DEFAULT_TEMPLATE_BODY);
    const [versionChecksum, setVersionChecksum] = useState('');
    const [versionMetadata, setVersionMetadata] = useState(DEFAULT_METADATA_SCHEMA);
    const [versionNotes, setVersionNotes] = useState('');
    const handleCreateTemplate = async (event) => {
        event.preventDefault();
        const name = templateName.trim();
        const provider = templateProvider.trim();
        if (!name) {
            templateForm.showError('Template name is required');
            return;
        }
        if (!provider) {
            templateForm.showError('Provider is required');
            return;
        }
        try {
            setCreatingTemplate(true);
            templateForm.reset();
            const labels = templateLabels.trim() ? parseTemplateLabels(templateLabels) : undefined;
            await api.createTemplate({
                name,
                provider,
                description: templateDescription.trim() || undefined,
                labels,
            });
            setTemplateName('');
            setTemplateProvider('');
            setTemplateDescription('');
            setTemplateLabels('{}');
            setOffset(0);
            reload();
            templateForm.showSuccess('Template created successfully');
            showToast('Template created', 'success');
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to create template';
            templateForm.showError(message);
            showToast(message, 'error');
        }
        finally {
            setCreatingTemplate(false);
        }
    };
    const handleCreateVersion = async (event) => {
        event.preventDefault();
        if (!selectedTemplate) {
            versionForm.showError('Select a template first');
            return;
        }
        const body = versionBody.trim();
        if (!body) {
            versionForm.showError('Version body is required');
            return;
        }
        try {
            setCreatingVersion(true);
            versionForm.reset();
            const payload = {
                body,
                checksum: versionChecksum.trim() || undefined,
                metadata_schema: parseJsonInput(versionMetadata),
                rollout_notes: versionNotes.trim() || undefined,
            };
            await api.createTemplateVersion(selectedTemplate.id, payload);
            reloadVersions();
            reload();
            versionForm.showSuccess('Version created');
            showToast('Template version created', 'success');
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to create version';
            versionForm.showError(message);
            showToast(message, 'error');
        }
        finally {
            setCreatingVersion(false);
        }
    };
    const handlePromoteVersion = async (versionNumber) => {
        if (!selectedTemplate) {
            return;
        }
        try {
            await api.promoteTemplateVersion(selectedTemplate.id, versionNumber);
            reloadVersions();
            reload();
            showToast(`Version ${versionNumber} promoted`, 'success');
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to promote version';
            showToast(message, 'error');
        }
    };
    const summary = useMemo(() => {
        const total = pagination.total;
        const promoted = templates.filter((tpl) => tpl.promoted_version?.version).length;
        const archived = templates.filter((tpl) => Boolean(tpl.archived_at)).length;
        return { total, promoted, archived };
    }, [pagination.total, templates]);
    const handleToggleArchived = async () => {
        if (!selectedTemplate) {
            return;
        }
        setUpdatingTemplate(true);
        const nextArchived = !selectedTemplate.archived_at;
        try {
            await api.updateTemplate(selectedTemplate.id, { archived: nextArchived });
            showToast(nextArchived ? 'Template archived.' : 'Template restored.', nextArchived ? 'info' : 'success');
            reload();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to update template.';
            showToast(message, 'error');
        }
        finally {
            setUpdatingTemplate(false);
        }
    };
    return (_jsxs("section", { className: "templates-page", children: [_jsx("div", { className: "page-header", children: _jsxs("div", { children: [_jsx("h2", { children: "Infrastructure templates" }), _jsx("p", { children: "Version, test, and safely roll out infrastructure changes. Each template is reusable across tenants." })] }) }), _jsxs("div", { className: "stat-card-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total templates" }), _jsx("strong", { children: summary.total })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Promoted" }), _jsx("strong", { children: summary.promoted }), _jsx("small", { className: "muted", children: "stable rollouts" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Archived" }), _jsx("strong", { children: summary.archived }), _jsx("small", { className: "muted", children: "hidden from provisioning" })] })] }), _jsxs("form", { className: "panel templates-form", onSubmit: handleCreateTemplate, children: [_jsx("h3", { children: "Create template" }), _jsxs("div", { className: "grid two-col", children: [_jsxs("label", { htmlFor: "template-name", children: ["Name", _jsx("input", { id: "template-name", type: "text", value: templateName, onChange: (event) => setTemplateName(event.target.value), placeholder: "e.g. aws-foundation", disabled: creatingTemplate, required: true })] }), _jsxs("label", { htmlFor: "template-provider", children: ["Provider", _jsx("input", { id: "template-provider", type: "text", value: templateProvider, onChange: (event) => setTemplateProvider(event.target.value), placeholder: "aws|azure|gcp|mock", disabled: creatingTemplate, required: true })] })] }), _jsxs("label", { htmlFor: "template-description", children: ["Description", _jsx("textarea", { id: "template-description", rows: 3, value: templateDescription, onChange: (event) => setTemplateDescription(event.target.value), placeholder: "High-level purpose", disabled: creatingTemplate })] }), _jsxs("label", { htmlFor: "template-labels", children: ["Labels (JSON)", _jsx("textarea", { id: "template-labels", rows: 3, value: templateLabels, onChange: (event) => setTemplateLabels(event.target.value), disabled: creatingTemplate })] }), templateForm.error ? _jsx("p", { className: "form-error", children: templateForm.error }) : null, templateForm.success ? _jsx("p", { className: "form-success", children: templateForm.success }) : null, _jsx("button", { type: "submit", disabled: creatingTemplate, children: creatingTemplate ? 'Saving…' : 'Create template' })] }), _jsxs("div", { className: "panel toolbar templates-toolbar", children: [_jsxs("label", { htmlFor: "name-filter", children: ["Name prefix", _jsx("input", { id: "name-filter", type: "text", value: nameFilter, onChange: (event) => {
                                    setNameFilter(event.target.value);
                                    setOffset(0);
                                }, placeholder: "search by name" })] }), _jsxs("label", { htmlFor: "provider-filter", children: ["Provider", _jsx("input", { id: "provider-filter", type: "text", value: providerFilter, onChange: (event) => {
                                    setProviderFilter(event.target.value);
                                    setOffset(0);
                                }, placeholder: "aws" })] }), _jsxs("label", { htmlFor: "include-archived", className: "checkbox-inline", children: [_jsx("input", { id: "include-archived", type: "checkbox", checked: includeArchived, onChange: (event) => {
                                    setIncludeArchived(event.target.checked);
                                    setOffset(0);
                                } }), "Include archived"] })] }), loading ? _jsx("p", { className: "muted", children: "Loading templates\u2026" }) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load templates: ", error] }) : null, !loading && !error && templates.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { className: "muted", children: "No templates found. Create one to get started." }) })) : null, !loading && templates.length > 0 ? (_jsxs("div", { className: "templates-layout", children: [_jsxs("div", { className: "templates-list", children: [_jsxs("table", { className: "templates-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Provider" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Updated" }), _jsx("th", {})] }) }), _jsx("tbody", { children: templates.map((template) => (_jsxs("tr", { className: template.id === selectedTemplateId ? 'active-row' : '', children: [_jsx("td", { children: template.name }), _jsx("td", { children: template.provider || '—' }), _jsx("td", { children: _jsx("span", { className: "badge", children: templateStatus(template) }) }), _jsx("td", { children: formatDate(template.updated_at) }), _jsx("td", { children: _jsx("button", { type: "button", onClick: () => setSelectedTemplateId(template.id), children: "View" }) })] }, template.id))) })] }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: pagination.prevOffset === null || pagination.prevOffset === undefined, onClick: () => setOffset(pagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", templates.length, " of ", pagination.total, " templates"] }), _jsx("button", { type: "button", disabled: pagination.nextOffset === null || pagination.nextOffset === undefined, onClick: () => setOffset(pagination.nextOffset ?? offset + limit), children: "Next" })] })] }), _jsx("aside", { className: "panel templates-detail", children: selectedTemplate ? (_jsxs(_Fragment, { children: [_jsxs("header", { children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: selectedTemplate.provider.toUpperCase() }), _jsx("h3", { children: selectedTemplate.name })] }), _jsx("span", { className: "badge", children: templateStatus(selectedTemplate) })] }), selectedTemplate.description ? _jsx("p", { children: selectedTemplate.description }) : null, _jsxs("dl", { className: "meta-grid", children: [_jsxs("div", { children: [_jsx("dt", { children: "Template ID" }), _jsx("dd", { children: selectedTemplate.id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(selectedTemplate.created_at) })] }), _jsxs("div", { children: [_jsx("dt", { children: "Updated" }), _jsx("dd", { children: formatDate(selectedTemplate.updated_at) })] }), _jsxs("div", { children: [_jsx("dt", { children: "Archived" }), _jsx("dd", { children: selectedTemplate.archived_at ? formatDate(selectedTemplate.archived_at) : '—' })] })] }), _jsxs("section", { children: [_jsx("h4", { children: "Labels" }), selectedTemplate.labels && Object.keys(selectedTemplate.labels).length > 0 ? (_jsx("ul", { className: "label-list", children: Object.entries(selectedTemplate.labels).map(([key, value]) => (_jsxs("li", { children: [_jsx("strong", { children: key }), _jsx("span", { children: value })] }, key))) })) : (_jsx("p", { className: "muted", children: "No labels assigned." }))] }), _jsxs("section", { children: [_jsx("h4", { children: "Versions" }), versionsLoading ? _jsx("p", { className: "muted", children: "Loading versions\u2026" }) : null, versionsError ? _jsx("p", { className: "form-error", children: versionsError }) : null, !versionsLoading && versions.length === 0 ? (_jsx("p", { className: "muted", children: "No versions published yet." })) : null, !versionsLoading && versions.length > 0 ? (_jsx("ul", { className: "versions-list", children: versions.map((version) => (_jsxs("li", { children: [_jsxs("div", { children: [_jsxs("strong", { children: ["v", version.version] }), " \u2022 ", formatDate(version.created_at), version.promoted_at ? (_jsx("span", { className: "badge badge-success", children: "Promoted" })) : null] }), version.rollout_notes ? _jsx("p", { children: version.rollout_notes }) : null, _jsx("div", { className: "version-actions", children: _jsx("button", { type: "button", onClick: () => handlePromoteVersion(version.version), children: "Promote" }) })] }, version.id))) })) : null, _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: versionPagination.prevOffset === null || versionPagination.prevOffset === undefined, onClick: () => setVersionOffset(versionPagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", versions.length, " of ", versionPagination.total, " versions"] }), _jsx("button", { type: "button", disabled: versionPagination.nextOffset === null || versionPagination.nextOffset === undefined, onClick: () => setVersionOffset(versionPagination.nextOffset ?? versionOffset + versionLimit), children: "Next" })] })] }), _jsxs("section", { children: [_jsx("h4", { children: "Create version" }), _jsxs("form", { onSubmit: handleCreateVersion, className: "version-form", children: [_jsxs("label", { htmlFor: "version-body", children: ["Body (JSON/YAML)", _jsx("textarea", { id: "version-body", rows: 6, value: versionBody, onChange: (event) => setVersionBody(event.target.value), disabled: creatingVersion })] }), _jsxs("label", { htmlFor: "version-checksum", children: ["Checksum", _jsx("input", { id: "version-checksum", type: "text", value: versionChecksum, onChange: (event) => setVersionChecksum(event.target.value), disabled: creatingVersion })] }), _jsxs("label", { htmlFor: "version-metadata", children: ["Metadata schema (JSON)", _jsx("textarea", { id: "version-metadata", rows: 4, value: versionMetadata, onChange: (event) => setVersionMetadata(event.target.value), disabled: creatingVersion })] }), _jsxs("label", { htmlFor: "version-notes", children: ["Rollout notes", _jsx("textarea", { id: "version-notes", rows: 3, value: versionNotes, onChange: (event) => setVersionNotes(event.target.value), disabled: creatingVersion })] }), versionForm.error ? _jsx("p", { className: "form-error", children: versionForm.error }) : null, versionForm.success ? _jsx("p", { className: "form-success", children: versionForm.success }) : null, _jsx("button", { type: "submit", disabled: creatingVersion, children: creatingVersion ? 'Publishing…' : 'Create version' })] })] }), _jsxs("div", { className: "detail-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: reload, disabled: loading, children: loading ? 'Refreshing…' : 'Refresh list' }), _jsx("button", { type: "button", className: selectedTemplate.archived_at ? 'primary-button' : 'danger-button', onClick: handleToggleArchived, disabled: updatingTemplate, children: updatingTemplate
                                                ? 'Updating…'
                                                : selectedTemplate.archived_at
                                                    ? 'Restore template'
                                                    : 'Archive template' })] })] })) : (_jsx("p", { className: "muted", children: "Select a template to inspect versions and metadata." })) })] })) : null] }));
}
