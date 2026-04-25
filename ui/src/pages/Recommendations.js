import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
export function Recommendations() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tenantId, setTenantId] = useState('');
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
            const resp = await client.listRecommendations(tenantId);
            setItems(resp.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
    }, [client, tenantId]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const promote = async (rec) => {
        if (rec.kind !== 'port_rule') {
            window.alert('Promote only supported for port_rule drafts in this build.');
            return;
        }
        const d = rec.draft;
        await client.createPortRule({
            tenant_id: tenantId,
            name: String(rec.title),
            port: Number(d.port),
            protocol: String(d.protocol),
            expected_state: String(d.expected_state),
            severity: String(d.severity ?? 'medium'),
            action: String(d.action ?? 'notify'),
            enabled: true,
        });
        window.alert('Promoted to port rule.');
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Behavioral" }), _jsx("h2", { children: "Recommendations" }), _jsx("p", { className: "subtitle", children: "Derived from 30 days of port observations." })] }), _jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: tenants.map((t) => _jsx("option", { value: t.id, children: t.name }, t.id)) })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, items.length === 0 ? (_jsx("p", { className: "muted", children: "No recommendations yet. Data needs \u226530 days of observations." })) : (_jsx("ul", { style: { listStyle: 'none', padding: 0 }, children: items.map((rec, i) => (_jsxs("li", { style: { border: '1px solid var(--border)', padding: '0.75rem', marginBottom: '0.5rem', borderRadius: 4 }, children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' }, children: [_jsxs("div", { children: [_jsx("strong", { children: rec.title }), _jsx("div", { className: "muted", children: rec.rationale }), _jsxs("small", { children: ["Confidence: ", (rec.confidence * 100).toFixed(1), "%"] })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => promote(rec), children: "Promote" })] }), _jsxs("details", { style: { marginTop: '0.5rem' }, children: [_jsx("summary", { children: "Draft" }), _jsx("pre", { children: JSON.stringify(rec.draft, null, 2) })] })] }, i))) }))] }));
}
