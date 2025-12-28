import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
export function Nodes() {
    const { data: tenants } = useTenants();
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const { data: nodes, loading, error } = useNodes(selectedTenant ? { tenantId: selectedTenant } : {});
    const tenantOptions = useMemo(() => tenants, [tenants]);
    const tenantNames = useMemo(() => {
        const entries = new Map();
        for (const tenant of tenants) {
            entries.set(tenant.id, tenant.name);
        }
        return entries;
    }, [tenants]);
    return (_jsxs("section", { children: [_jsx("h2", { children: "Nodes" }), _jsx("p", { children: "Connected agents reporting into the control plane." }), _jsxs("div", { className: "toolbar", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Filter by tenant" }), _jsxs("select", { id: "tenant-filter", value: selectedTenant ?? '', onChange: (event) => {
                            const value = event.target.value;
                            setSelectedTenant(value === '' ? undefined : value);
                        }, children: [_jsx("option", { value: "", children: "All tenants" }), tenantOptions.map((tenant) => (_jsx("option", { value: tenant.id, children: tenant.name }, tenant.id)))] })] }), loading ? _jsx("p", { className: "muted", children: "Loading nodes\u2026" }) : null, error ? _jsxs("p", { className: "form-error", children: ["Failed to load nodes: ", error] }) : null, !loading && !error && nodes.length === 0 ? _jsx("p", { className: "muted", children: "No nodes registered." }) : null, !loading && !error && nodes.length > 0 ? (_jsx("div", { className: "card-grid", children: nodes.map((node) => (_jsxs("article", { className: "node-card", children: [_jsxs("header", { children: [_jsx("h3", { children: node.hostname }), _jsx("span", { className: "badge", children: node.os ?? 'unknown OS' })] }), _jsxs("dl", { children: [_jsxs("div", { children: [_jsx("dt", { children: "Node ID" }), _jsx("dd", { children: node.id })] }), _jsxs("div", { children: [_jsx("dt", { children: "Tenant" }), _jsx("dd", { children: tenantNames.get(node.tenant_id) ?? node.tenant_id })] }), node.public_ip ? (_jsxs("div", { children: [_jsx("dt", { children: "Public IP" }), _jsx("dd", { children: node.public_ip })] })) : null, node.arch ? (_jsxs("div", { children: [_jsx("dt", { children: "Architecture" }), _jsx("dd", { children: node.arch })] })) : null] })] }, node.id))) })) : null] }));
}
