import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
function formatDate(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return value;
    }
    return date.toLocaleString();
}
export function Tenants() {
    const api = useApiClient();
    const [offset, setOffset] = useState(0);
    const [limit] = useState(20);
    const [nameFilter, setNameFilter] = useState('');
    const { data, pagination, loading, error, reload } = useTenants({
        limit,
        offset,
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
        const name = tenantName.trim();
        if (!name) {
            showError('Tenant name is required');
            return;
        }
        setSubmitting(true);
        reset();
        try {
            await api.createTenant({ name });
            setTenantName('');
            setOffset(0);
            reload();
            const successMessage = 'Tenant created successfully.';
            showSuccess(successMessage);
            showToast(successMessage, 'success');
        }
        catch (err) {
            if (err instanceof Error) {
                showError(err.message);
                showToast(err.message, 'error');
            }
            else {
                const fallback = 'Failed to create tenant';
                showError(fallback);
                showToast(fallback, 'error');
            }
        }
        finally {
            setSubmitting(false);
        }
    };
    const openTenantDetails = (tenantId) => {
        setSelectedTenantId((current) => (current === tenantId ? null : tenantId));
        const tenant = rows.find((t) => t.id === tenantId);
        setRenameValue(tenant?.name ?? '');
    };
    const handleRenameTenant = async () => {
        if (!selectedTenant) {
            return;
        }
        const next = renameValue.trim();
        if (!next) {
            showToast('Tenant name cannot be empty.', 'error');
            return;
        }
        if (next === selectedTenant.name) {
            showToast('No changes detected.', 'info');
            return;
        }
        setRenaming(true);
        try {
            await api.updateTenant(selectedTenant.id, { name: next });
            showToast('Tenant renamed.', 'success');
            reload();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to rename tenant.';
            showToast(message, 'error');
        }
        finally {
            setRenaming(false);
        }
    };
    const handleDeleteTenant = async () => {
        if (!selectedTenant) {
            return;
        }
        const confirmed = window.confirm(`Delete tenant “${selectedTenant.name}”? Nodes and jobs referencing this tenant may become orphaned.`);
        if (!confirmed) {
            return;
        }
        setDeleting(true);
        try {
            await api.deleteTenant(selectedTenant.id);
            showToast('Tenant deleted.', 'success');
            setSelectedTenantId(null);
            reload();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to delete tenant.';
            showToast(message, 'error');
        }
        finally {
            setDeleting(false);
        }
    };
    return (_jsxs("section", { className: "tenants-page", children: [_jsx("div", { className: "page-header", children: _jsxs("div", { children: [_jsx("h2", { children: "Tenants" }), _jsx("p", { children: "Tenants represent isolation boundaries for infrastructure, policy, and compliance scope." })] }) }), _jsxs("div", { className: "stat-card-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total tenants" }), _jsx("strong", { children: summary.total })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Most recent" }), _jsx("strong", { children: summary.newestName }), _jsx("small", { className: "muted", children: summary.newestDate })] })] }), _jsxs("div", { className: "tenants-layout", children: [_jsxs("form", { className: "panel tenants-form", onSubmit: handleCreateTenant, children: [_jsx("h3", { children: "Create tenant" }), _jsx("label", { htmlFor: "tenant-name", children: "Name" }), _jsx("input", { id: "tenant-name", name: "tenant-name", type: "text", value: tenantName, onChange: (event) => setTenantName(event.target.value), placeholder: "e.g. Production Cluster", disabled: submitting, required: true }), formError ? _jsx("p", { className: "form-error", children: formError }) : null, formSuccess ? _jsx("p", { className: "form-success", children: formSuccess }) : null, _jsx("button", { type: "submit", disabled: submitting, children: submitting ? 'Creating…' : 'Create tenant' })] }), _jsxs("div", { className: "panel tenants-list", children: [_jsxs("div", { className: "toolbar tenants-toolbar", children: [_jsxs("label", { htmlFor: "tenant-search", children: ["Filter", _jsx("input", { id: "tenant-search", type: "search", placeholder: "Search by name", value: nameFilter, onChange: (event) => {
                                                    setNameFilter(event.target.value);
                                                    setOffset(0);
                                                } })] }), _jsx("button", { type: "button", className: "ghost-button", onClick: () => {
                                            reload();
                                        }, disabled: loading, children: loading ? 'Refreshing…' : 'Refresh' })] }), loading ? _jsx("p", { className: "muted", children: "Loading tenants\u2026" }) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load tenants: ", error] }) : null, !loading && !error && rows.length === 0 ? _jsx("p", { className: "muted", children: "No tenants match the current filters." }) : null, !loading && !error && rows.length > 0 ? (_jsxs(_Fragment, { children: [_jsxs("table", { className: "tenants-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Tenant ID" }), _jsx("th", { children: "Created" }), _jsx("th", {})] }) }), _jsx("tbody", { children: rows.map((tenant) => (_jsxs("tr", { className: selectedTenantId === tenant.id ? 'active-row' : undefined, children: [_jsx("td", { children: tenant.name }), _jsx("td", { children: tenant.id }), _jsx("td", { children: formatDate(tenant.created_at) }), _jsx("td", { children: _jsx("button", { type: "button", className: "ghost-button", onClick: () => openTenantDetails(tenant.id), children: selectedTenantId === tenant.id ? 'Hide' : 'View' }) })] }, tenant.id))) })] }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: pagination.prevOffset === null || pagination.prevOffset === undefined, onClick: () => setOffset(pagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", rows.length, " of ", pagination.total, " tenants"] }), _jsx("button", { type: "button", disabled: pagination.nextOffset === null || pagination.nextOffset === undefined, onClick: () => setOffset(pagination.nextOffset ?? offset + limit), children: "Next" })] })] })) : null] }), selectedTenant ? (_jsxs("aside", { className: "panel tenant-detail", children: [_jsx("h3", { children: "Tenant details" }), _jsxs("dl", { className: "meta-grid", children: [_jsxs("div", { children: [_jsx("dt", { children: "Name" }), _jsx("dd", { children: selectedTenant.name })] }), _jsxs("div", { children: [_jsx("dt", { children: "Tenant ID" }), _jsx("dd", { className: "mono", children: selectedTenant.id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(selectedTenant.created_at) })] })] }), _jsxs("div", { className: "tenant-detail-form", children: [_jsx("label", { htmlFor: "rename-tenant", children: "Rename tenant" }), _jsx("input", { id: "rename-tenant", type: "text", value: renameValue, onChange: (event) => setRenameValue(event.target.value) }), _jsxs("div", { className: "detail-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedTenantId(null), children: "Close" }), _jsx("button", { type: "button", className: "primary-button", onClick: handleRenameTenant, disabled: renaming, children: renaming ? 'Saving…' : 'Save changes' }), _jsx("button", { type: "button", className: "danger-button", onClick: handleDeleteTenant, disabled: deleting, children: deleting ? 'Deleting…' : 'Delete tenant' })] })] })] })) : null] })] }));
}
