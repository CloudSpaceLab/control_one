import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
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
export function Nodes() {
    const api = useApiClient();
    const { data: tenants, reload: reloadTenants } = useTenants();
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const [hostnameFilter, setHostnameFilter] = useState('');
    const [limit] = useState(12);
    const [offset, setOffset] = useState(0);
    const { data: nodes, loading, error, pagination, reload: reloadNodes } = useNodes({
        tenantId: selectedTenant,
        hostnamePrefix: hostnameFilter.trim() || undefined,
        limit,
        offset,
    });
    const [formTenantId, setFormTenantId] = useState('');
    const [formTenantName, setFormTenantName] = useState('');
    const [hostname, setHostname] = useState('');
    const [os, setOs] = useState('');
    const [arch, setArch] = useState('');
    const [publicIp, setPublicIp] = useState('');
    const [bootstrapToken, setBootstrapToken] = useState('');
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { showToast } = useToast();
    const [registering, setRegistering] = useState(false);
    const [selectedNodeId, setSelectedNodeId] = useState(null);
    const [detailHostname, setDetailHostname] = useState('');
    const [detailOs, setDetailOs] = useState('');
    const [detailArch, setDetailArch] = useState('');
    const [detailPublicIp, setDetailPublicIp] = useState('');
    const [updating, setUpdating] = useState(false);
    const [deleting, setDeleting] = useState(false);
    const tenantOptions = useMemo(() => tenants, [tenants]);
    const tenantNames = useMemo(() => {
        const entries = new Map();
        for (const tenant of tenants) {
            entries.set(tenant.id, tenant.name);
        }
        return entries;
    }, [tenants]);
    const selectedNode = useMemo(() => nodes.find((node) => node.id === selectedNodeId) ?? null, [nodes, selectedNodeId]);
    const summary = useMemo(() => {
        return {
            total: pagination.total,
            filtered: nodes.length,
        };
    }, [pagination.total, nodes.length]);
    const handleRegisterNode = async (event) => {
        event.preventDefault();
        const trimmedHostname = hostname.trim();
        const trimmedToken = bootstrapToken.trim();
        const trimmedTenantName = formTenantName.trim();
        if (!trimmedHostname) {
            showError('Hostname is required');
            return;
        }
        if (!trimmedToken) {
            showError('Bootstrap token is required');
            return;
        }
        if (!formTenantId && !trimmedTenantName) {
            showError('Select an existing tenant or provide a new tenant name');
            return;
        }
        setRegistering(true);
        resetFeedback();
        try {
            const payload = {
                hostname: trimmedHostname,
                bootstrap_token: trimmedToken,
            };
            if (formTenantId) {
                payload.tenant_id = formTenantId;
            }
            else if (trimmedTenantName) {
                payload.tenant_name = trimmedTenantName;
            }
            if (os.trim()) {
                payload.os = os.trim();
            }
            if (arch.trim()) {
                payload.arch = arch.trim();
            }
            if (publicIp.trim()) {
                payload.public_ip = publicIp.trim();
            }
            const response = await api.registerNode(payload);
            const successMessage = `Node ${response.node_id} registered for tenant ${response.tenant_id}.`;
            showSuccess(successMessage);
            showToast(successMessage, 'success');
            setHostname('');
            setOs('');
            setArch('');
            setPublicIp('');
            setBootstrapToken('');
            setFormTenantName('');
            setSelectedTenant(response.tenant_id);
            setFormTenantId(response.tenant_id);
            reloadNodes();
            reloadTenants();
        }
        catch (err) {
            if (err instanceof Error) {
                showError(err.message);
                showToast(err.message, 'error');
            }
            else {
                const fallback = 'Failed to register node';
                showError(fallback);
                showToast(fallback, 'error');
            }
        }
        finally {
            setRegistering(false);
        }
    };
    const openNodeDetails = (nodeId) => {
        setSelectedNodeId((current) => (current === nodeId ? null : nodeId));
        const node = nodes.find((n) => n.id === nodeId);
        setDetailHostname(node?.hostname ?? '');
        setDetailOs(node?.os ?? '');
        setDetailArch(node?.arch ?? '');
        setDetailPublicIp(node?.public_ip ?? '');
    };
    const handleUpdateNode = async () => {
        if (!selectedNode) {
            return;
        }
        const payload = {};
        const trimmedHostname = detailHostname.trim();
        const trimmedOs = detailOs.trim();
        const trimmedArch = detailArch.trim();
        const trimmedPublicIp = detailPublicIp.trim();
        if (trimmedHostname && trimmedHostname !== selectedNode.hostname) {
            payload.hostname = trimmedHostname;
        }
        if (trimmedOs !== (selectedNode.os ?? '')) {
            payload.os = trimmedOs;
        }
        if (trimmedArch !== (selectedNode.arch ?? '')) {
            payload.arch = trimmedArch;
        }
        if (trimmedPublicIp !== (selectedNode.public_ip ?? '')) {
            payload.public_ip = trimmedPublicIp;
        }
        if (!payload.hostname && payload.os === undefined && payload.arch === undefined && payload.public_ip === undefined) {
            showToast('No changes to save.', 'info');
            return;
        }
        setUpdating(true);
        try {
            await api.updateNode(selectedNode.id, payload);
            showToast('Node updated.', 'success');
            await reloadNodes();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to update node.';
            showToast(message, 'error');
        }
        finally {
            setUpdating(false);
        }
    };
    const handleDeleteNode = async () => {
        if (!selectedNode) {
            return;
        }
        const confirmed = window.confirm(`Delete node “${selectedNode.hostname}”?`);
        if (!confirmed) {
            return;
        }
        setDeleting(true);
        try {
            await api.deleteNode(selectedNode.id);
            showToast('Node deleted.', 'success');
            setSelectedNodeId(null);
            await reloadNodes();
        }
        catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to delete node.';
            showToast(message, 'error');
        }
        finally {
            setDeleting(false);
        }
    };
    return (_jsxs("section", { className: "nodes-page", children: [_jsx("div", { className: "page-header", children: _jsxs("div", { children: [_jsx("h2", { children: "Nodes" }), _jsx("p", { children: "Connected agents reporting into the control plane." })] }) }), _jsxs("div", { className: "stat-card-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total nodes" }), _jsx("strong", { children: summary.total }), _jsx("small", { className: "muted", children: selectedTenant ? `Filtered by tenant` : 'All tenants' })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Visible" }), _jsx("strong", { children: summary.filtered }), _jsx("small", { className: "muted", children: "matching current filters" })] })] }), _jsxs("div", { className: "nodes-layout", children: [_jsxs("form", { className: "panel nodes-form", onSubmit: handleRegisterNode, children: [_jsx("h3", { children: "Register node" }), _jsx("label", { htmlFor: "register-tenant", children: "Existing tenant" }), _jsxs("select", { id: "register-tenant", value: formTenantId, onChange: (event) => {
                                    setFormTenantId(event.target.value);
                                }, disabled: registering, children: [_jsx("option", { value: "", children: "\u2014 Select tenant \u2014" }), tenantOptions.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] }), _jsx("small", { className: "muted", children: "Optionally provide a new tenant name below to auto-create a tenant during registration." }), _jsx("label", { htmlFor: "new-tenant-name", children: "New tenant name" }), _jsx("input", { id: "new-tenant-name", type: "text", placeholder: "e.g. Edge Cluster", value: formTenantName, onChange: (event) => setFormTenantName(event.target.value), disabled: registering }), _jsx("label", { htmlFor: "hostname", children: "Hostname" }), _jsx("input", { id: "hostname", type: "text", value: hostname, onChange: (event) => setHostname(event.target.value), placeholder: "node-01.example.com", disabled: registering, required: true }), _jsx("label", { htmlFor: "node-os", children: "Operating system" }), _jsx("input", { id: "node-os", type: "text", value: os, onChange: (event) => setOs(event.target.value), placeholder: "Ubuntu 24.04", disabled: registering }), _jsx("label", { htmlFor: "node-arch", children: "Architecture" }), _jsx("input", { id: "node-arch", type: "text", value: arch, onChange: (event) => setArch(event.target.value), placeholder: "x86_64", disabled: registering }), _jsx("label", { htmlFor: "node-ip", children: "Public IP" }), _jsx("input", { id: "node-ip", type: "text", value: publicIp, onChange: (event) => setPublicIp(event.target.value), placeholder: "203.0.113.10", disabled: registering }), _jsx("label", { htmlFor: "bootstrap-token", children: "Bootstrap token" }), _jsx("input", { id: "bootstrap-token", type: "text", value: bootstrapToken, onChange: (event) => setBootstrapToken(event.target.value), placeholder: "control-one-bootstrap-token", disabled: registering, required: true }), formError ? _jsx("p", { className: "form-error", children: formError }) : null, formSuccess ? _jsx("p", { className: "form-success", children: formSuccess }) : null, _jsx("button", { type: "submit", disabled: registering, children: registering ? 'Registering…' : 'Register node' })] }), _jsxs("div", { className: "panel nodes-list", children: [_jsxs("div", { className: "toolbar nodes-toolbar", children: [_jsxs("label", { htmlFor: "tenant-filter", children: ["Tenant", _jsxs("select", { id: "tenant-filter", value: selectedTenant ?? '', onChange: (event) => {
                                                    const value = event.target.value;
                                                    setSelectedTenant(value === '' ? undefined : value);
                                                    setOffset(0);
                                                }, children: [_jsx("option", { value: "", children: "All tenants" }), tenantOptions.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] })] }), _jsxs("label", { htmlFor: "hostname-filter", children: ["Hostname", _jsx("input", { id: "hostname-filter", type: "search", placeholder: "Search hostname", value: hostnameFilter, onChange: (event) => {
                                                    setHostnameFilter(event.target.value);
                                                    setOffset(0);
                                                } })] }), _jsx("button", { type: "button", className: "ghost-button", onClick: reloadNodes, disabled: loading, children: loading ? 'Refreshing…' : 'Refresh' })] }), loading ? _jsx("p", { className: "muted", children: "Loading nodes\u2026" }) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load nodes: ", error] }) : null, !loading && !error && nodes.length === 0 ? _jsx("p", { className: "muted", children: "No nodes match the current filters." }) : null, !loading && !error && nodes.length > 0 ? (_jsxs(_Fragment, { children: [_jsxs("table", { className: "nodes-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Hostname" }), _jsx("th", { children: "Tenant" }), _jsx("th", { children: "OS" }), _jsx("th", { children: "Public IP" }), _jsx("th", {})] }) }), _jsx("tbody", { children: nodes.map((node) => (_jsxs("tr", { className: selectedNodeId === node.id ? 'active-row' : undefined, children: [_jsx("td", { children: node.hostname }), _jsx("td", { children: tenantNames.get(node.tenant_id) ?? node.tenant_id }), _jsx("td", { children: node.os ?? '—' }), _jsx("td", { children: node.public_ip ?? '—' }), _jsx("td", { children: _jsx("button", { type: "button", className: "ghost-button", onClick: () => openNodeDetails(node.id), children: selectedNodeId === node.id ? 'Hide' : 'View' }) })] }, node.id))) })] }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", disabled: pagination.prevOffset === null || pagination.prevOffset === undefined, onClick: () => setOffset(pagination.prevOffset ?? 0), children: "Previous" }), _jsxs("span", { children: ["Showing ", nodes.length, " of ", pagination.total, " nodes"] }), _jsx("button", { type: "button", disabled: pagination.nextOffset === null || pagination.nextOffset === undefined, onClick: () => setOffset(pagination.nextOffset ?? offset + limit), children: "Next" })] })] })) : null] }), selectedNode ? (_jsxs("aside", { className: "panel node-detail", children: [_jsx("h3", { children: "Node details" }), _jsxs("dl", { className: "meta-grid", children: [_jsxs("div", { children: [_jsx("dt", { children: "Hostname" }), _jsx("dd", { children: selectedNode.hostname })] }), _jsxs("div", { children: [_jsx("dt", { children: "Node ID" }), _jsx("dd", { className: "mono", children: selectedNode.id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Tenant" }), _jsx("dd", { children: tenantNames.get(selectedNode.tenant_id) ?? selectedNode.tenant_id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Created" }), _jsx("dd", { children: formatDate(selectedNode.created_at) })] }), _jsxs("div", { children: [_jsx("dt", { children: "Updated" }), _jsx("dd", { children: formatDate(selectedNode.updated_at) })] })] }), _jsxs("div", { className: "node-detail-form", children: [_jsx("label", { htmlFor: "detail-hostname", children: "Hostname" }), _jsx("input", { id: "detail-hostname", type: "text", value: detailHostname, onChange: (event) => setDetailHostname(event.target.value) }), _jsx("label", { htmlFor: "detail-os", children: "Operating system" }), _jsx("input", { id: "detail-os", type: "text", value: detailOs, onChange: (event) => setDetailOs(event.target.value), placeholder: "Ubuntu 24.04" }), _jsx("label", { htmlFor: "detail-arch", children: "Architecture" }), _jsx("input", { id: "detail-arch", type: "text", value: detailArch, onChange: (event) => setDetailArch(event.target.value), placeholder: "x86_64" }), _jsx("label", { htmlFor: "detail-ip", children: "Public IP" }), _jsx("input", { id: "detail-ip", type: "text", value: detailPublicIp, onChange: (event) => setDetailPublicIp(event.target.value), placeholder: "203.0.113.10" }), _jsxs("div", { className: "detail-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedNodeId(null), children: "Close" }), _jsx("button", { type: "button", className: "primary-button", onClick: handleUpdateNode, disabled: updating, children: updating ? 'Saving…' : 'Save changes' }), _jsx("button", { type: "button", className: "danger-button", onClick: handleDeleteNode, disabled: deleting, children: deleting ? 'Deleting…' : 'Delete node' })] })] })] })) : null] })] }));
}
