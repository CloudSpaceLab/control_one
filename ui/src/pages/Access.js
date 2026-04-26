import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
export function Access() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tenantId, setTenantId] = useState('');
    const [tab, setTab] = useState('pending');
    const [items, setItems] = useState([]);
    const [error, setError] = useState(null);
    useEffect(() => {
        if (!tenantId && tenants[0]?.id)
            setTenantId(tenants[0].id);
    }, [tenants, tenantId]);
    const refresh = useCallback(async () => {
        if (!tenantId)
            return;
        try {
            const status = tab === 'pending' ? 'pending' : undefined;
            const resp = await client.listAccessRequests({ tenantId, status, limit: 100, offset: 0 });
            setItems(resp.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
    }, [client, tenantId, tab]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const approve = async (id) => {
        await client.approveAccessRequest(id, '');
        refresh();
    };
    const deny = async (id) => {
        await client.denyAccessRequest(id, '');
        refresh();
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Just-in-time access" }), _jsx("h2", { children: "Privileged access requests" }), _jsx("p", { className: "subtitle", children: "Request, approve, auto-revoke. No standing admin credentials." })] }), _jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: tenants.map((t) => _jsx("option", { value: t.id, children: t.name }, t.id)) })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("div", { className: "tab-row", role: "tablist", style: { display: 'flex', gap: '0.5rem', marginBottom: '1rem' }, children: [_jsx("button", { type: "button", role: "tab", "aria-selected": tab === 'pending', className: tab === 'pending' ? 'primary-button' : 'secondary-button', onClick: () => setTab('pending'), children: "Pending" }), _jsx("button", { type: "button", role: "tab", "aria-selected": tab === 'all', className: tab === 'all' ? 'primary-button' : 'secondary-button', onClick: () => setTab('all'), children: "All" })] }), _jsx(RequestForm, { tenantId: tenantId, onCreated: refresh }), _jsxs("table", { className: "data-table", style: { marginTop: '1rem', width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Type" }), _jsx("th", { children: "Access" }), _jsx("th", { children: "Justification" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Requested" }), _jsx("th", { children: "Expires" }), _jsx("th", {})] }) }), _jsx("tbody", { children: items.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 7, className: "muted", children: "No requests." }) })) : (items.map((req) => (_jsxs("tr", { children: [_jsx("td", { children: req.target_resource_type }), _jsx("td", { children: _jsx("code", { children: req.requested_access }) }), _jsx("td", { children: req.justification ?? '—' }), _jsx("td", { children: req.status }), _jsx("td", { children: new Date(req.requested_at).toLocaleString() }), _jsx("td", { children: req.expires_at ? new Date(req.expires_at).toLocaleString() : '—' }), _jsx("td", { children: req.status === 'pending' ? (_jsxs(_Fragment, { children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => approve(req.id), children: "Approve" }), _jsx("button", { type: "button", className: "secondary-button", onClick: () => deny(req.id), style: { marginLeft: '0.4rem' }, children: "Deny" })] })) : null })] }, req.id)))) })] })] }));
}
function RequestForm({ tenantId, onCreated }) {
    const client = useApiClient();
    const [form, setForm] = useState({
        tenant_id: tenantId,
        target_resource_type: 'ssh',
        requested_access: 'root',
        justification: '',
        ttl_seconds: 1800,
    });
    const [submitting, setSubmitting] = useState(false);
    useEffect(() => {
        setForm((f) => ({ ...f, tenant_id: tenantId }));
    }, [tenantId]);
    const submit = async (e) => {
        e.preventDefault();
        if (!tenantId)
            return;
        setSubmitting(true);
        try {
            await client.createAccessRequest(form);
            setForm({ ...form, justification: '' });
            onCreated();
        }
        finally {
            setSubmitting(false);
        }
    };
    return (_jsxs("form", { onSubmit: submit, style: { display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: '0.5rem', alignItems: 'end' }, children: [_jsxs("label", { htmlFor: "ar-type", children: ["Type", _jsxs("select", { id: "ar-type", value: form.target_resource_type, onChange: (e) => setForm({ ...form, target_resource_type: e.target.value }), children: [_jsx("option", { value: "ssh", children: "ssh" }), _jsx("option", { value: "rdp", children: "rdp" }), _jsx("option", { value: "db", children: "db" })] })] }), _jsxs("label", { htmlFor: "ar-access", children: ["Access", _jsx("input", { id: "ar-access", required: true, value: form.requested_access, onChange: (e) => setForm({ ...form, requested_access: e.target.value }) })] }), _jsxs("label", { htmlFor: "ar-justification", style: { gridColumn: 'span 2' }, children: ["Justification", _jsx("input", { id: "ar-justification", value: form.justification ?? '', onChange: (e) => setForm({ ...form, justification: e.target.value }) })] }), _jsxs("label", { htmlFor: "ar-ttl", children: ["TTL (s)", _jsx("input", { id: "ar-ttl", type: "number", min: 60, value: form.ttl_seconds ?? 1800, onChange: (e) => setForm({ ...form, ttl_seconds: Number(e.target.value) }) })] }), _jsx("button", { type: "submit", className: "primary-button", disabled: submitting, style: { gridColumn: 'span 5' }, children: "Request access" })] }));
}
