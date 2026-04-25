import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
const RANGE_PRESETS = [
    { label: 'Last 24h', days: 1 },
    { label: 'Last 7 days', days: 7 },
    { label: 'Last 30 days', days: 30 },
    { label: 'Last 90 days', days: 90 },
];
export function Reports() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tenantId, setTenantId] = useState('');
    const [reports, setReports] = useState([]);
    const [range, setRange] = useState(30);
    const [error, setError] = useState(null);
    useEffect(() => {
        if (!tenantId && tenants[0]?.id)
            setTenantId(tenants[0].id);
    }, [tenants, tenantId]);
    useEffect(() => {
        (async () => {
            try {
                const resp = await client.listReports();
                setReports(resp.data);
            }
            catch (err) {
                setError(err instanceof Error ? err.message : 'load failed');
            }
        })();
    }, [client]);
    const download = (slug) => {
        const since = new Date(Date.now() - range * 24 * 60 * 60 * 1000).toISOString();
        const url = client.buildReportExportUrl(slug, { tenantId, since });
        window.open(url, '_blank');
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Exports" }), _jsx("h2", { children: "Reports" }), _jsx("p", { className: "subtitle", children: "Download CSV extracts for compliance, audit, alerts, and access." })] }), _jsxs("div", { style: { display: 'flex', gap: '0.5rem' }, children: [_jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: tenants.map((t) => _jsx("option", { value: t.id, children: t.name }, t.id)) }), _jsx("select", { value: range, onChange: (e) => setRange(Number(e.target.value)), "aria-label": "Range", children: RANGE_PRESETS.map((p) => _jsx("option", { value: p.days, children: p.label }, p.days)) })] })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1rem' }, children: reports.map((rep) => (_jsxs("article", { className: "stat-card", style: { padding: '1rem' }, children: [_jsx("p", { style: { marginTop: 0 }, children: _jsx("strong", { children: rep.title }) }), _jsx("p", { className: "muted", style: { minHeight: '3em' }, children: rep.description }), _jsx("div", { style: { display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }, children: rep.formats.map((fmt) => (_jsxs("button", { type: "button", className: "primary-button", onClick: () => download(rep.slug), children: ["Download ", fmt.toUpperCase()] }, fmt))) })] }, rep.slug))) })] }));
}
