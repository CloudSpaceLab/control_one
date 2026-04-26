import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import { useFleetSummary } from '../hooks/useFleetSummary';
import TopologyGrid from '../components/glyphs/TopologyGrid';
import StatusDot from '../components/glyphs/StatusDot';
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
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Last 24 hours" }), _jsx("h2", { children: "Your infrastructure at a glance" }), _jsx("p", { className: "subtitle", children: loading ? 'Loading…' : `Updated ${new Date(overview.generated_at || Date.now()).toLocaleTimeString()}` })] }), _jsx("button", { type: "button", className: "primary-button", onClick: refresh, children: loading ? 'Refreshing…' : 'Refresh' })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("div", { className: "stat-grid", children: [_jsx(SeverityCard, { title: "Security events (24h)", breakdown: overview.security_event_counts }), _jsx(SeverityCard, { title: "Open health incidents", breakdown: overview.health_incident_counts }), _jsx(CountCard, { title: "Compliance alerts (24h)", total: overview.compliance_summary.failed, sub: `${overview.compliance_summary.passed} passed / ${overview.compliance_summary.total} total`, onClick: () => navigate('/compliance') }), _jsx(CountCard, { title: "Rule triggers (24h)", total: totalRuleTriggers, sub: Object.entries(overview.rule_trigger_counts_24h ?? {})
                            .map(([k, v]) => `${k}:${v}`)
                            .join(' · ') || '—', onClick: () => navigate('/rules') }), _jsx(CountCard, { title: "Auto-remediations (24h)", total: overview.remediations_applied_24h, sub: "Safety gates active" }), _jsx(CountCard, { title: "Nodes", total: overview.node_counts.total, sub: `${overview.node_counts.healthy} healthy · ${overview.node_counts.offline} offline`, onClick: () => navigate('/nodes') })] }), _jsx(FleetTopologyCard, { onNodeClick: (n) => navigate(`/nodes?focus=${encodeURIComponent(n.id)}`) }), _jsx("div", { className: "dashboard-panels", children: _jsxs("article", { className: "quick-actions", children: [_jsx("h3", { children: "Quick actions" }), _jsxs("ul", { children: [_jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Author a rule" }), _jsx("p", { children: "Compliance, port, or log rule \u2014 rolls out in realtime." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/rules'), children: "Go" })] }), _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Register hypervisor" }), _jsx("p", { children: "Add a KVM / VMware / AWS / Azure host to provision from." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/hypervisors'), children: "Go" })] }), _jsxs("li", { children: [_jsxs("div", { children: [_jsx("strong", { children: "Enroll a node" }), _jsx("p", { children: "Run the one-line installer or bulk-enroll via SSH." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => navigate('/nodes'), children: "Go" })] })] })] }) })] }));
}
function SeverityCard({ title, breakdown }) {
    return (_jsxs("article", { className: "stat-card", children: [_jsx("p", { children: title }), _jsx("span", { className: "stat-value", children: breakdown.total }), _jsxs("small", { children: [breakdown.critical, " crit \u00B7 ", breakdown.high, " high \u00B7 ", breakdown.medium, " med \u00B7 ", breakdown.low, " low"] })] }));
}
function CountCard({ title, total, sub, onClick, }) {
    return (_jsxs("article", { className: "stat-card", onClick: onClick, role: onClick ? 'button' : undefined, tabIndex: onClick ? 0 : undefined, onKeyDown: onClick ? (e) => { if (e.key === 'Enter' || e.key === ' ')
            onClick(); } : undefined, style: onClick ? { cursor: 'pointer' } : undefined, children: [_jsx("p", { children: title }), _jsx("span", { className: "stat-value", children: total }), _jsx("small", { children: sub })] }));
}
// FleetTopologyCard — every node as a colour dot. Tap to drill in. Adapts
// from 5 nodes to thousands without code changes; --state-* tokens drive
// the colour and the pulse on critical so accessibility wins for free.
function FleetTopologyCard({ onNodeClick }) {
    const { data, loading, error } = useFleetSummary({ intervalMs: 30000 });
    const nodes = (data?.nodes ?? []).map((n) => ({
        id: n.node_id,
        hostname: n.hostname,
        state: (n.state ?? 'unknown'),
        hint: `${n.hostname ?? n.node_id} · cpu ${Math.round((n.cpu_p95 ?? 0) * 100)}% · mem ${Math.round((n.mem_p95 ?? 0) * 100)}% · ${n.conn_count ?? 0} conns · ${n.alerts_open ?? 0} alerts`,
    }));
    const totals = data?.totals ?? {
        nodes: 0, healthy: 0, warning: 0, degraded: 0, critical: 0, unknown: 0,
    };
    return (_jsxs("article", { className: "card", style: { padding: 16, marginTop: 24 }, children: [_jsxs("header", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }, children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Fleet topology" }), _jsxs("h3", { style: { marginTop: 4 }, children: [totals.nodes, " nodes \u00B7 live"] })] }), _jsxs("div", { style: { display: 'flex', gap: 12, fontSize: 12 }, children: [_jsx(Legend, { label: "Healthy", state: "healthy", count: totals.healthy }), _jsx(Legend, { label: "Warning", state: "warning", count: totals.warning }), _jsx(Legend, { label: "Degraded", state: "degraded", count: totals.degraded }), _jsx(Legend, { label: "Critical", state: "critical", count: totals.critical }), _jsx(Legend, { label: "Unknown", state: "unknown", count: totals.unknown })] })] }), error ? (_jsxs("p", { style: { color: 'var(--state-critical)', fontSize: 13 }, children: ["Topology offline: ", error.message] })) : null, loading ? _jsx("p", { style: { color: 'var(--text-secondary)', fontSize: 13 }, children: "Syncing\u2026" }) : null, _jsx(TopologyGrid, { nodes: nodes, onNodeClick: onNodeClick }), data?.source === 'postgres-fallback' ? (_jsx("small", { style: { color: 'var(--state-warning)', display: 'block', marginTop: 8 }, children: "Fast view \u2014 Doris unavailable, sourced from Postgres rollups." })) : null] }));
}
function Legend({ label, state, count }) {
    return (_jsxs("span", { style: { display: 'inline-flex', alignItems: 'center', gap: 4 }, children: [_jsx(StatusDot, { state: state }), _jsx("span", { style: { color: 'var(--text-secondary)' }, children: label }), _jsx("strong", { children: count })] }));
}
