import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMemo, useState, useCallback } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { NodeDiscovery } from '../components/NodeDiscovery';
import { DemandForm } from '../components/DemandForm';
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
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { showToast } = useToast();
    const [registering, setRegistering] = useState(false);
    const [formTenantId, setFormTenantId] = useState('');
    const [formTenantName, setFormTenantName] = useState('');
    const [hostname, setHostname] = useState('');
    const [os, setOs] = useState('');
    const [arch, setArch] = useState('');
    const [publicIp, setPublicIp] = useState('');
    const [bootstrapToken, setBootstrapToken] = useState('');
    const [selectedNodeId, setSelectedNodeId] = useState(null);
    const [editHostname, setEditHostname] = useState('');
    const [editOs, setEditOs] = useState('');
    const [editPublicIp, setEditPublicIp] = useState('');
    const [updating, setUpdating] = useState(false);
    const [deleting, setDeleting] = useState(false);
    const handleDiscoveredNodes = useCallback((discoveredNodes) => {
        // Auto-fill form with first discovered node
        if (discoveredNodes.length > 0) {
            const node = discoveredNodes[0];
            setHostname(node.ip);
            setPublicIp(node.ip);
            setOs(node.os || 'Unknown');
            showToast(`Discovered ${discoveredNodes.length} node(s)`, 'success');
        }
    }, [showToast]);
    const tenantOptions = useMemo(() => tenants, [tenants]);
    const tenantNames = useMemo(() => new Map(tenants.map((t) => [t.id, t.name])), [tenants]);
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
        setEditHostname(node?.hostname ?? '');
        setEditOs(node?.os ?? '');
        setEditPublicIp(node?.public_ip ?? '');
    };
    const handleUpdateNode = async () => {
        if (!selectedNode) {
            return;
        }
        const payload = {};
        const trimmedHostname = editHostname.trim();
        const trimmedOs = editOs.trim();
        const trimmedPublicIp = editPublicIp.trim();
        if (trimmedHostname && trimmedHostname !== selectedNode.hostname) {
            payload.hostname = trimmedHostname;
        }
        if (trimmedOs !== (selectedNode.os ?? '')) {
            payload.os = trimmedOs;
        }
        if (trimmedPublicIp !== (selectedNode.public_ip ?? '')) {
            payload.public_ip = trimmedPublicIp;
        }
        if (!payload.hostname && payload.os === undefined && payload.public_ip === undefined) {
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
    return (_jsxs("div", { className: "focused-content", children: [_jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDDA5\uFE0F Node Management" }), _jsx("p", { className: "focused-section-subtitle", children: "Connected agents reporting into the control plane." })] }), _jsx("div", { className: "focused-section-content", children: _jsxs("div", { className: "stat-grid", children: [_jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Total nodes" }), _jsx("strong", { children: summary.total }), _jsx("small", { className: "muted", children: selectedTenant ? `Filtered by tenant` : 'All tenants' })] }), _jsxs("article", { className: "stat-card", children: [_jsx("span", { className: "muted", children: "Visible" }), _jsx("strong", { children: summary.filtered }), _jsx("small", { className: "muted", children: "matching current filters" })] })] }) })] }), _jsx(DemandForm, { title: "Filters", icon: "\uD83D\uDD0D", summary: `${summary.filtered} nodes shown`, children: _jsxs("div", { className: "compact-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Filter by hostname" }), _jsx("input", { type: "text", placeholder: "e.g. node-01", value: hostnameFilter, onChange: (event) => setHostnameFilter(event.target.value) })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Tenant" }), _jsxs("select", { value: selectedTenant || '', onChange: (event) => setSelectedTenant(event.target.value || undefined), children: [_jsx("option", { value: "", children: "All tenants" }), tenantOptions.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] })] })] }) }), _jsx(DemandForm, { title: "Smart Discovery", icon: "\uD83D\uDD0D", summary: "Find and auto-register nodes", children: _jsx(NodeDiscovery, { onNodesDiscovered: handleDiscoveredNodes }) }), _jsx(DemandForm, { title: "Quick Register", icon: "\u2795", summary: "Register a new node manually", children: _jsxs("form", { className: "compact-form", onSubmit: handleRegisterNode, children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "hostname", children: "Hostname" }), _jsx("input", { id: "hostname", type: "text", value: hostname, onChange: (event) => setHostname(event.target.value), placeholder: "node-01.example.com", disabled: registering, required: true })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "node-os", children: "OS" }), _jsx("input", { id: "node-os", type: "text", value: os, onChange: (event) => setOs(event.target.value), placeholder: "Ubuntu 24.04", disabled: registering })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "node-arch", children: "Architecture" }), _jsx("input", { id: "node-arch", type: "text", value: arch, onChange: (event) => setArch(event.target.value), placeholder: "x86_64", disabled: registering })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "new-tenant-name", children: "New tenant (optional)" }), _jsx("input", { id: "new-tenant-name", type: "text", placeholder: "e.g. Edge Cluster", value: formTenantName, onChange: (event) => setFormTenantName(event.target.value), disabled: registering })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "bootstrap-token", children: "Bootstrap token" }), _jsx("input", { id: "bootstrap-token", type: "text", value: bootstrapToken, onChange: (event) => setBootstrapToken(event.target.value), placeholder: "Auto-generated if empty", disabled: registering })] }), _jsx("button", { type: "submit", className: "primary-button", disabled: registering, children: registering ? 'Registering…' : 'Register Node' })] }) }), _jsxs("div", { className: "focused-section", children: [_jsxs("div", { className: "focused-section-header", children: [_jsx("h2", { className: "focused-section-title", children: "\uD83D\uDCCB Node Registry" }), _jsx("p", { className: "focused-section-subtitle", children: "Manage and monitor connected nodes" })] }), _jsx("div", { className: "focused-section-content", children: nodes.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No nodes match the current filters." }) })) : (_jsx("div", { className: "nodes-list", children: nodes.map((node) => (_jsxs("div", { className: "node-card", children: [_jsxs("header", { children: [_jsx("h3", { children: node.hostname }), _jsx("div", { className: `status-dot status-online` })] }), _jsxs("dl", { children: [_jsx("dt", { children: "Tenant" }), _jsx("dd", { children: node.tenant_id }), _jsx("dt", { children: "OS" }), _jsx("dd", { children: node.os || '—' }), _jsx("dt", { children: "Architecture" }), _jsx("dd", { children: node.arch || '—' }), _jsx("dt", { children: "Public IP" }), _jsx("dd", { children: node.public_ip || '—' }), _jsx("dt", { children: "Last seen" }), _jsx("dd", { children: formatDate(node.updated_at) })] }), _jsxs("div", { className: "node-card-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: () => setSelectedNodeId(node.id), disabled: deleting, children: selectedNode?.id === node.id ? 'Close' : 'Manage' }), _jsx("button", { type: "button", className: "danger-button", onClick: handleDeleteNode, disabled: deleting || selectedNode?.id !== node.id, children: "Delete" })] })] }, node.id))) })) })] }), selectedNode && (_jsx(DemandForm, { title: `Manage: ${selectedNode.hostname}`, icon: "\u2699\uFE0F", summary: "Update node configuration", defaultExpanded: true, children: _jsx("div", { className: "node-detail", children: _jsxs("div", { className: "node-detail-form", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "edit-hostname", children: "Hostname" }), _jsx("input", { id: "edit-hostname", type: "text", value: editHostname, onChange: (event) => setEditHostname(event.target.value), disabled: updating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "edit-os", children: "Operating system" }), _jsx("input", { id: "edit-os", type: "text", value: editOs, onChange: (event) => setEditOs(event.target.value), disabled: updating })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "edit-public-ip", children: "Public IP" }), _jsx("input", { id: "edit-public-ip", type: "text", value: editPublicIp, onChange: (event) => setEditPublicIp(event.target.value), disabled: updating })] }), _jsxs("div", { className: "detail-actions", children: [_jsx("button", { type: "button", className: "primary-button", onClick: handleUpdateNode, disabled: updating, children: updating ? 'Updating…' : 'Update Node' }), _jsx("button", { type: "button", className: "danger-button", onClick: handleDeleteNode, disabled: deleting, children: deleting ? 'Deleting…' : 'Delete Node' })] })] }) }) }))] }));
}
