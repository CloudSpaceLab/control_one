import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useSecretGroups, useSecretSyncs } from '../hooks/useSecrets';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import './Secrets.css';
function formatDate(value) {
    if (!value) {
        return '—';
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return value;
    }
    return parsed.toLocaleString();
}
export function Secrets() {
    const api = useApiClient();
    const [limit] = useState(50);
    const [offset, setOffset] = useState(0);
    const [selectedTenant] = useState(undefined);
    const [selectedGroupId, setSelectedGroupId] = useState(null);
    const [isCreating, setIsCreating] = useState(false);
    const [isSyncing, setIsSyncing] = useState(false);
    useTenants();
    const { data: groups, loading: groupsLoading, error: groupsError, pagination, reload: reloadGroups, } = useSecretGroups({
        tenant_id: selectedTenant,
        limit,
        offset,
    });
    const { data: syncs, loading: syncsLoading, error: syncsError, reload: reloadSyncs, } = useSecretSyncs(selectedGroupId, { limit: 20, offset: 0 });
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { showToast } = useToast();
    const [saving, setSaving] = useState(false);
    const [formData, setFormData] = useState({
        name: '',
        backend: 'vault',
        endpoint: '',
        sync_interval_seconds: 900,
    });
    const handleCreate = () => {
        setIsCreating(true);
        setFormData({
            name: '',
            backend: 'vault',
            endpoint: '',
            sync_interval_seconds: 900,
        });
        resetFeedback();
    };
    const handleCancel = () => {
        setIsCreating(false);
        setFormData({
            name: '',
            backend: 'vault',
            endpoint: '',
            sync_interval_seconds: 900,
        });
        resetFeedback();
    };
    const handleSubmit = async (event) => {
        event.preventDefault();
        if (!formData.name.trim()) {
            showError('Name is required');
            return;
        }
        setSaving(true);
        resetFeedback();
        try {
            const payload = {
                ...formData,
                tenant_id: selectedTenant || undefined,
            };
            await api.createSecretGroup(payload);
            showSuccess('Secret group created successfully');
            setIsCreating(false);
            reloadGroups();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to create secret group';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setSaving(false);
        }
    };
    const handleDelete = async (groupId) => {
        if (!confirm('Are you sure you want to delete this secret group?')) {
            return;
        }
        try {
            await api.deleteSecretGroup(groupId);
            showToast('Secret group deleted successfully', 'success');
            reloadGroups();
            if (selectedGroupId === groupId) {
                setSelectedGroupId(null);
            }
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to delete secret group';
            showToast(message, 'error');
        }
    };
    const handleSync = async (groupId) => {
        setIsSyncing(true);
        try {
            await api.syncSecretGroup(groupId);
            showToast('Secret sync triggered successfully', 'success');
            reloadGroups();
            if (selectedGroupId === groupId) {
                reloadSyncs();
            }
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to sync secrets';
            showToast(message, 'error');
        }
        finally {
            setIsSyncing(false);
        }
    };
    const selectedGroup = groups.find((g) => g.id === selectedGroupId) || null;
    return (_jsxs("div", { className: "secrets-page", children: [_jsxs("div", { className: "page-header", children: [_jsxs("div", { children: [_jsx("h1", { children: "Secrets vault" }), _jsx("p", { className: "subtitle", children: "Encrypted credentials. Tracked rotations. Audit-ready access logs." })] }), _jsx("div", { className: "page-actions", children: _jsx("button", { type: "button", onClick: handleCreate, className: "btn-primary", children: "Create Secret Group" }) })] }), groupsError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading secret groups: ", groupsError] }) })), _jsxs("div", { className: "secrets-stats", children: [_jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: pagination.total }), _jsx("div", { className: "stat-label", children: "Total Groups" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: groups.filter((g) => g.sync_status === 'success').length }), _jsx("div", { className: "stat-label", children: "Synced" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: groups.filter((g) => g.sync_status === 'failed').length }), _jsx("div", { className: "stat-label", children: "Failed" })] })] }), _jsxs("div", { className: "content-grid", children: [_jsxs("div", { className: "groups-section", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Secret Groups" }), _jsxs("div", { className: "results-count", children: ["Showing ", groups.length, " of ", pagination.total] })] }), groupsLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading secret groups..." })) : groups.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No secret groups found." }) })) : (_jsx("div", { className: "table-container", children: _jsxs("table", { className: "groups-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Backend" }), _jsx("th", { children: "Sync Status" }), _jsx("th", { children: "Last Sync" }), _jsx("th", { children: "Actions" })] }) }), _jsx("tbody", { children: groups.map((group) => (_jsxs("tr", { className: selectedGroupId === group.id ? 'selected' : '', onClick: () => setSelectedGroupId(group.id), children: [_jsxs("td", { children: [_jsx("div", { className: "group-name", children: group.name }), group.endpoint && (_jsx("div", { className: "group-endpoint", children: group.endpoint }))] }), _jsx("td", { children: _jsx("span", { className: "backend-badge", children: group.backend }) }), _jsxs("td", { children: [_jsx("span", { className: `status-pill status-${group.sync_status}`, children: group.sync_status }), group.sync_error && (_jsx("div", { className: "sync-error", title: group.sync_error, children: "\u26A0\uFE0F" }))] }), _jsx("td", { children: formatDate(group.last_sync_at) }), _jsx("td", { children: _jsxs("div", { className: "action-buttons", children: [_jsx("button", { type: "button", onClick: (e) => {
                                                                        e.stopPropagation();
                                                                        handleSync(group.id);
                                                                    }, className: "btn-link", disabled: isSyncing, children: "Sync" }), _jsx("button", { type: "button", onClick: (e) => {
                                                                        e.stopPropagation();
                                                                        handleDelete(group.id);
                                                                    }, className: "btn-link danger", children: "Delete" })] }) })] }, group.id))) })] }) })), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || groupsLoading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(pagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= pagination.total || groupsLoading, className: "btn-secondary", children: "Next" })] })] }), selectedGroup && (_jsxs("div", { className: "syncs-section", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Sync History" }), _jsx("button", { type: "button", onClick: () => setSelectedGroupId(null), className: "btn-link", children: "Close" })] }), _jsxs("div", { className: "group-details", children: [_jsx("h3", { children: selectedGroup.name }), _jsxs("dl", { children: [_jsx("dt", { children: "Backend" }), _jsx("dd", { children: selectedGroup.backend }), _jsx("dt", { children: "Endpoint" }), _jsx("dd", { children: selectedGroup.endpoint || '—' }), _jsx("dt", { children: "Sync Status" }), _jsx("dd", { children: _jsx("span", { className: `status-pill status-${selectedGroup.sync_status}`, children: selectedGroup.sync_status }) }), _jsx("dt", { children: "Last Sync" }), _jsx("dd", { children: formatDate(selectedGroup.last_sync_at) })] })] }), syncsLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading sync history..." })) : syncsError ? (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading sync history: ", syncsError] }) })) : syncs.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No sync history available." }) })) : (_jsx("div", { className: "table-container", children: _jsxs("table", { className: "syncs-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Secret Path" }), _jsx("th", { children: "Version" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Synced At" })] }) }), _jsx("tbody", { children: syncs.map((sync) => (_jsxs("tr", { children: [_jsx("td", { children: sync.secret_path }), _jsx("td", { children: sync.secret_version || '—' }), _jsx("td", { children: _jsx("span", { className: `status-pill status-${sync.sync_status}`, children: sync.sync_status }) }), _jsx("td", { children: formatDate(sync.synced_at) })] }, sync.id))) })] }) }))] }))] }), isCreating && (_jsx("div", { className: "modal-overlay", onClick: handleCancel, children: _jsxs("div", { className: "modal-content", onClick: (e) => e.stopPropagation(), children: [_jsxs("div", { className: "modal-header", children: [_jsx("h2", { children: "Create Secret Group" }), _jsx("button", { type: "button", onClick: handleCancel, className: "modal-close", children: "\u00D7" })] }), _jsxs("form", { onSubmit: handleSubmit, children: [_jsxs("div", { className: "modal-body", children: [formError && (_jsx("div", { className: "error-banner", children: _jsx("p", { children: formError }) })), formSuccess && (_jsx("div", { className: "success-banner", children: _jsx("p", { children: formSuccess }) })), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "name", children: "Name *" }), _jsx("input", { id: "name", type: "text", value: formData.name, onChange: (e) => setFormData({ ...formData, name: e.target.value }), required: true })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "backend", children: "Backend *" }), _jsx("select", { id: "backend", value: formData.backend, onChange: (e) => setFormData({ ...formData, backend: e.target.value }), required: true, children: _jsx("option", { value: "vault", children: "Vault" }) })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "endpoint", children: "Endpoint" }), _jsx("input", { id: "endpoint", type: "text", value: formData.endpoint, onChange: (e) => setFormData({ ...formData, endpoint: e.target.value }), placeholder: "secret/data/app" })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "sync_interval", children: "Sync Interval (seconds)" }), _jsx("input", { id: "sync_interval", type: "number", value: formData.sync_interval_seconds, onChange: (e) => setFormData({
                                                        ...formData,
                                                        sync_interval_seconds: parseInt(e.target.value, 10) || 900,
                                                    }), min: "60" })] })] }), _jsxs("div", { className: "modal-footer", children: [_jsx("button", { type: "button", onClick: handleCancel, className: "btn-secondary", disabled: saving, children: "Cancel" }), _jsx("button", { type: "submit", className: "btn-primary", disabled: saving, children: saving ? 'Creating...' : 'Create' })] })] })] }) }))] }));
}
