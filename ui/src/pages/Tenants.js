import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { DemandForm } from '../components/DemandForm';
function formatDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return value;
    }
    return date.toLocaleString();
}
export function Tenants() {
    const api = useApiClient();
    const [limit] = useState(20);
    const [nameFilter, setNameFilter] = useState('');
    const { data, pagination, loading, reload } = useTenants({
        limit,
        namePrefix: nameFilter.trim() || undefined,
    });
    const [tenantName, setTenantName] = useState('');
    const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();
    const { showToast } = useToast();
    const [submitting, setSubmitting] = useState(false);
    const [selectedTenantId, setSelectedTenantId] = useState(null);
    const [renameValue, setRenameValue] = useState('');
    const [renaming, setRenaming] = useState(false);
    const [deleting, setDeleting] = useState(false);
    const rows = useMemo(() => data, [data]);
    const selectedTenant = useMemo(() => rows.find((tenant) => tenant.id === selectedTenantId) ?? null, [rows, selectedTenantId]);
    const summary = useMemo(() => {
        const total = pagination.total;
        const newest = rows[0];
        return {
            total,
            newestName: newest?.name ?? '—',
            newestDate: newest ? formatDate(newest.created_at) : '—',
        };
    }, [pagination.total, rows]);
    const handleCreateTenant = async (event) => {
        event.preventDefault();
        const trimmedName = tenantName.trim();
        if (!trimmedName) {
            showError('Tenant name is required');
            return;
        }
        setSubmitting(true);
        reset();
        try {
            await api.createTenant({ name: trimmedName });
            showSuccess(`Tenant "${trimmedName}" created successfully.`);
            showToast(`Tenant "${trimmedName}" created successfully.`, 'success');
            setTenantName('');
            reload();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to create tenant.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setSubmitting(false);
        }
    };
    const handleRenameTenant = async () => {
        if (!selectedTenant || !renameValue.trim()) {
            showError('Tenant name is required');
            return;
        }
        setRenaming(true);
        try {
            await api.updateTenant(selectedTenant.id, { name: renameValue.trim() });
            showSuccess(`Tenant renamed to "${renameValue.trim()}".`);
            showToast('Tenant renamed successfully.', 'success');
            setRenameValue('');
            setSelectedTenantId(null);
            reload();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to rename tenant.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setRenaming(false);
        }
    };
    const handleDeleteTenant = async () => {
        if (!selectedTenant)
            return;
        const confirmed = window.confirm(`Delete tenant "${selectedTenant.name}"? This action cannot be undone.`);
        if (!confirmed)
            return;
        setDeleting(true);
        try {
            await api.deleteTenant(selectedTenant.id);
            showSuccess(`Tenant "${selectedTenant.name}" deleted successfully.`);
            showToast('Tenant deleted successfully.', 'success');
            setSelectedTenantId(null);
            reload();
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to delete tenant.';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setDeleting(false);
        }
    };
    return (_jsxs("div", { className: "focused-content", children: [_jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83C\uDFE2 Tenant Management" }), _jsx("p", { className: "focused-section-subtitle", children: "Manage workspaces and environments for your organization." })] }), _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "stat-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total Tenants" }), _jsx("strong", { children: summary.total }), _jsx("small", { className: "muted", children: "All environments" })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Newest Tenant" }), _jsx("strong", { children: summary.newestName }), _jsx("small", { className: "muted", children: summary.newestDate })] })] }) })] }), _jsx(DemandForm, { title: "Create New Tenant", icon: "\u2795", summary: "Add a new workspace or environment", children: _jsxs("form", { onSubmit: handleCreateTenant, children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "tenant-name", children: "Tenant Name" }), _jsx("input", { id: "tenant-name", type: "text", value: tenantName, onChange: (e) => setTenantName(e.target.value), placeholder: "e.g. Production Environment", disabled: submitting, required: true })] }), formError && _jsx("div", { className: "form-error", children: formError }), formSuccess && _jsx("div", { className: "form-success", children: formSuccess }), _jsx("button", { type: "submit", className: "primary-button", disabled: submitting, children: submitting ? 'Creating…' : 'Create Tenant' })] }) }), _jsx(DemandForm, { title: "Filters", icon: "\uD83D\uDD0D", summary: `${rows.length} tenants shown`, children: _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "name-filter", children: "Filter by name" }), _jsx("input", { id: "name-filter", type: "text", value: nameFilter, onChange: (e) => setNameFilter(e.target.value), placeholder: "e.g. Production" })] }) }), _jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCCB Tenant Registry" }), _jsx("p", { className: "focused-section-subtitle", children: "Manage and monitor all tenant environments" })] }), _jsx("div", { className: "focused-section-content", children: loading ? (_jsx("p", { className: "muted", children: "Loading tenants\u2026" })) : rows.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No tenants found. Create your first tenant to get started." }) })) : (_jsx("div", { className: "tenant-list", children: rows.map((tenant) => (_jsxs("div", { className: "tenant-card", children: [_jsxs("header", { children: [_jsx("h3", { children: tenant.name }), _jsxs("div", { className: "tenant-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: () => {
                                                            setSelectedTenantId(tenant.id);
                                                            setRenameValue(tenant.name);
                                                        }, disabled: renaming || deleting, children: "Rename" }), _jsx("button", { type: "button", className: "danger-button", onClick: () => setSelectedTenantId(tenant.id), disabled: renaming || deleting, children: "Delete" })] })] }), _jsxs("dl", { children: [_jsx("dt", { children: "ID" }), _jsx("dd", { children: tenant.id }), _jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(tenant.created_at) })] })] }, tenant.id))) })) })] }), selectedTenant && (_jsx(DemandForm, { title: `Manage: ${selectedTenant.name}`, icon: "\u2699\uFE0F", summary: "Update tenant configuration", defaultExpanded: true, children: _jsxs("div", { className: "tenant-management", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "rename-tenant", children: "Rename Tenant" }), _jsx("input", { id: "rename-tenant", type: "text", value: renameValue, onChange: (e) => setRenameValue(e.target.value), placeholder: "New tenant name", disabled: renaming })] }), _jsxs("div", { className: "form-actions", children: [_jsx("button", { type: "button", className: "primary-button", onClick: handleRenameTenant, disabled: renaming || !renameValue.trim(), children: renaming ? 'Renaming…' : 'Rename Tenant' }), _jsx("button", { type: "button", className: "danger-button", onClick: handleDeleteTenant, disabled: deleting || renaming, children: deleting ? 'Deleting…' : 'Delete Tenant' }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTenantId(null), children: "Cancel" })] })] }) }))] }));
}
