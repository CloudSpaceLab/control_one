import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
const REFRESH_TOPICS = [
    'security.event',
    'health.incident',
    'rule.triggered',
    'remediation.applied',
    'compliance.fired',
    'alert.opened',
];
const INITIAL_SEV = { critical: 0, high: 0, medium: 0, low: 0, total: 0 };
const INITIAL_OVERVIEW = {
    generated_at: '',
    node_counts: { total: 0, healthy: 0, offline: 0 },
    security_event_counts: INITIAL_SEV,
    health_incident_counts: INITIAL_SEV,
    compliance_summary: { total: 0, passed: 0, failed: 0 },
    rule_trigger_counts_24h: {},
    remediations_applied_24h: 0,
};
export function Dashboard() {
    const navigate = useNavigate();
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 1, offset: 0 });
    const tenantId = tenants[0]?.id;
    const [overview, setOverview] = useState(INITIAL_OVERVIEW);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState(null);
    const refresh = useCallback(async () => {
        try {
            const data = await client.getDashboardOverview(tenantId);
            setOverview(data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load dashboard');
        }
        finally {
            setLoading(false);
        }
    }, [client, tenantId]);
    useEffect(() => {
        let cancelled = false;
        (async () => {
            if (cancelled)
                return;
            await refresh();
        })();
        const poll = window.setInterval(() => {
            if (!cancelled)
                refresh();
        }, 30_000);
        return () => {
            cancelled = true;
            window.clearInterval(poll);
        };
    }, [refresh]);
    // Realtime refresh on incoming events.
    useEventStream(tenantId, REFRESH_TOPICS, () => {
        refresh();
    });
    const totalRuleTriggers = useMemo(() => Object.values(overview.rule_trigger_counts_24h ?? {}).reduce((a, b) => a + b, 0), [overview.rule_trigger_counts_24h]);
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Last 24 hours" }), _jsx("h2", { children: "Your infrastructure at a glance" }), _jsx("p", { className: "subtitle", children: loading ? 'Loading…' : `Updated ${new Date(overview.generated_at || Date.now()).toLocaleTimeString()}` })] }), _jsx("button", { type: "button", className: "primary-button", onClick: refresh, children: "Refresh" })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("div", { className: "stat-grid", children: [_jsx(SeverityCard, { title: "Security events (24h)", breakdown: overview.security_event_counts }), _jsx(SeverityCard, { title: "Open health incidents", breakdown: overview.health_incident_counts }), _jsx(CountCard, { title: "Compliance alerts (24h)", total: overview.compliance_summary.failed, sub: `${overview.compliance_summary.passed} passed / ${overview.compliance_summary.total} total`, onClick: () => navigate('/compliance') }), _jsx(CountCard, { title: "Rule triggers (24h)", total: totalRuleTriggers, sub: Object.entries(overview.rule_trigger_counts_24h ?? {})
                            .map(([k, v]) => `${k}:${v}`)
                            .join(' · ') || '—', onClick: () => navigate('/rules') }), _jsx(CountCard, { title: "Auto-remediations (24h)", total: overview.remediations_applied_24h, sub: "Safety gates active" }), _jsx(CountCard, { title: "Nodes", total: overview.node_counts.total, sub: `${overview.node_counts.healthy} healthy · ${overview.node_counts.offline} offline`, onClick: () => navigate('/nodes') })] }), _jsx("div", { className: "dashboard-panels", children: _jsxs("article", { className: "quick-actions", children: [_jsx("h3", { children: "Quick actions" }), _jsxs("ul", { children: [_jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Author a rule" }), _jsx("p", { children: "Compliance, port, or log rule \u2014 rolls out in realtime." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/rules'), children: "Go" })] }), _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Register hypervisor" }), _jsx("p", { children: "Add a KVM / VMware / AWS / Azure host to provision from." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/hypervisors'), children: "Go" })] }), _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Enroll a node" }), _jsx("p", { children: "Run the one-line installer or bulk-enroll via SSH." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/nodes'), children: "Go" })] })] })] }) })] }));
}
function SeverityCard({ title, breakdown }) {
    return (_jsxs("article", { className: "stat-card", children: [_jsx("p", { children: title }), _jsx("span", { className: "stat-value", children: breakdown.total }), _jsxs("small", { children: [breakdown.critical, " crit \u00B7 ", breakdown.high, " high \u00B7 ", breakdown.medium, " med \u00B7 ", breakdown.low, " low"] })] }));
}
function CountCard({ title, total, sub, onClick, }) {
    return (_jsxs("article", { className: "stat-card", onClick: onClick, style: onClick ? { cursor: 'pointer' } : undefined, children: [_jsx("p", { children: title }), _jsx("span", { className: "stat-value", children: total }), _jsx("small", { children: sub })] }));
}
