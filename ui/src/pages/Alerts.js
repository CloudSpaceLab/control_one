import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
const STATE_FILTERS = ['open', 'acked', 'resolved'];
export function Alerts() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tenantId, setTenantId] = useState('');
    const [state, setState] = useState('open');
    const [alerts, setAlerts] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    useEffect(() => {
        if (!tenantId && tenants[0]?.id)
            setTenantId(tenants[0].id);
    }, [tenants, tenantId]);
    const refresh = useCallback(async () => {
        if (!tenantId)
            return;
        setLoading(true);
        try {
            const resp = await client.listAlerts({ tenantId, state, limit: 100, offset: 0 });
            setAlerts(resp.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
        finally {
            setLoading(false);
        }
    }, [client, tenantId, state]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    useEventStream(tenantId, ['alert.opened'], () => refresh());
    const ack = async (id) => {
        await client.ackAlert(id);
        refresh();
    };
    const resolve = async (id) => {
        await client.resolveAlert(id);
        refresh();
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Operations" }), _jsx("h2", { children: "Alerts" }), _jsx("p", { className: "subtitle", children: "Deduped inbox from correlation, rules, and compliance." })] }), _jsxs("div", { style: { display: 'flex', gap: '0.5rem' }, children: [_jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: tenants.map((t) => _jsx("option", { value: t.id, children: t.name }, t.id)) }), _jsx("select", { value: state, onChange: (e) => setState(e.target.value), "aria-label": "State", children: STATE_FILTERS.map((s) => _jsx("option", { value: s, children: s }, s)) }), _jsx("button", { type: "button", className: "primary-button", onClick: refresh, children: "Refresh" })] })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("table", { className: "data-table", style: { width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Severity" }), _jsx("th", { children: "Title" }), _jsx("th", { children: "Source" }), _jsx("th", { children: "Opened" }), _jsx("th", { children: "State" }), _jsx("th", {})] }) }), _jsx("tbody", { children: loading ? (_jsx("tr", { children: _jsx("td", { colSpan: 6, className: "muted", children: "Loading\u2026" }) })) : alerts.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 6, className: "muted", children: "No alerts." }) })) : (alerts.map((a) => (_jsxs("tr", { children: [_jsx("td", { children: _jsx("span", { className: `status-pill status-${a.severity}`, children: a.severity }) }), _jsxs("td", { children: [_jsx("strong", { children: a.title }), a.summary ? _jsx("div", { className: "muted", children: a.summary }) : null] }), _jsx("td", { children: a.source }), _jsx("td", { children: new Date(a.opened_at).toLocaleString() }), _jsx("td", { children: a.state }), _jsxs("td", { children: [a.state === 'open' ? (_jsx("button", { type: "button", className: "secondary-button", onClick: () => ack(a.id), children: "Ack" })) : null, a.state !== 'resolved' ? (_jsx("button", { type: "button", className: "secondary-button", onClick: () => resolve(a.id), style: { marginLeft: '0.4rem' }, children: "Resolve" })) : null] })] }, a.id)))) })] })] }));
}
