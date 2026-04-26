import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { Badge } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import EventTimeline from '../components/EventTimeline';
import StatusDot from '../components/glyphs/StatusDot';
// Connections — full lifecycle table the user asked for. src/dst, pid,
// process, started/finished, duration, bytes_out, threat_match. Click a
// row → forensic timeline panel keyed by correlation_id.
export function Connections() {
    const client = useApiClient();
    const [rows, setRows] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [filter, setFilter] = useState({ threatOnly: false });
    const [selected, setSelected] = useState(null);
    const [detailLoading, setDetailLoading] = useState(false);
    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const resp = await client.listConnections({ ip: filter.ip, limit: 100 });
            setRows(filter.threatOnly ? resp.filter((r) => r.threat_match) : resp);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
        finally {
            setLoading(false);
        }
    }, [client, filter.ip, filter.threatOnly]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const openDetail = useCallback(async (row) => {
        setDetailLoading(true);
        try {
            const detail = await client.getConnectionDetail(row.conn_id);
            setSelected(detail);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
        finally {
            setDetailLoading(false);
        }
    }, [client]);
    const totals = useMemo(() => ({
        bytesOut: rows.reduce((s, r) => s + (r.bytes_out ?? 0), 0),
        bytesIn: rows.reduce((s, r) => s + (r.bytes_in ?? 0), 0),
        threatHits: rows.filter((r) => r.threat_match).length,
    }), [rows]);
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Network forensics" }), _jsx("h2", { children: "Connections" }), _jsx("p", { className: "subtitle", children: "Every external connection on every node \u2014 process, user, bytes in/out, duration. Click a row to see files touched, DB queries, and log events that share its correlation window." })] }), _jsxs("div", { style: { display: 'flex', gap: 8, alignItems: 'center' }, children: [_jsx("input", { type: "search", placeholder: "Filter by IP\u2026", value: filter.ip ?? '', onChange: (e) => setFilter((f) => ({ ...f, ip: e.target.value || undefined })), className: "search-input" }), _jsxs("label", { style: { display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13 }, children: [_jsx("input", { type: "checkbox", checked: filter.threatOnly, onChange: (e) => setFilter((f) => ({ ...f, threatOnly: e.target.checked })) }), "Threat-match only"] }), _jsx("button", { type: "button", className: "secondary-button", onClick: refresh, disabled: loading, children: loading ? 'Loading…' : 'Refresh' })] })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("div", { className: "card-grid", style: { gridTemplateColumns: 'repeat(3, 1fr)', marginBottom: 16 }, children: [_jsx(Stat, { label: "Total bytes out", value: formatBytes(totals.bytesOut), state: "warning" }), _jsx(Stat, { label: "Total bytes in", value: formatBytes(totals.bytesIn), state: "healthy" }), _jsx(Stat, { label: "Threat hits", value: String(totals.threatHits), state: totals.threatHits > 0 ? 'critical' : 'healthy' })] }), rows.length === 0 && !loading ? (_jsx(EmptyState, { title: "No connections in this window", description: "Once agents start streaming through the new ingest pipeline, every external connection on every node lands here." })) : (_jsxs("table", { className: "data-table", style: { width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Started" }), _jsx("th", { children: "Process" }), _jsx("th", { children: "User" }), _jsx("th", { children: "Source" }), _jsx("th", { children: "Destination" }), _jsx("th", { children: "Bytes out" }), _jsx("th", { children: "Duration" }), _jsx("th", { children: "Threat" })] }) }), _jsx("tbody", { children: rows.map((r) => (_jsxs("tr", { onClick: () => openDetail(r), style: { cursor: 'pointer' }, children: [_jsx("td", { children: formatTs(r.started_at) }), _jsxs("td", { children: [r.process_name ?? '—', r.pid ? _jsxs("small", { style: { color: 'var(--text-secondary)' }, children: [" \u00B7 pid ", r.pid] }) : null] }), _jsx("td", { children: r.user_name ?? '—' }), _jsxs("td", { children: [r.src_ip ?? '—', r.src_port ? `:${r.src_port}` : ''] }), _jsxs("td", { children: [r.dst_ip ?? '—', r.dst_port ? `:${r.dst_port}` : ''] }), _jsx("td", { children: formatBytes(r.bytes_out ?? 0) }), _jsx("td", { children: formatDuration(r.duration_ms) }), _jsx("td", { children: r.threat_match ? (_jsx(Badge, { variant: "critical", children: r.threat_feed ?? 'match' })) : (_jsx(StatusDot, { state: "healthy", title: "clean" })) })] }, r.conn_id))) })] })), selected ? (_jsx(DetailPanel, { detail: selected, loading: detailLoading, onClose: () => setSelected(null) })) : null] }));
}
function Stat({ label, value, state }) {
    return (_jsxs("div", { className: "card", style: { padding: 16 }, children: [_jsxs("div", { style: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }, children: [_jsx(StatusDot, { state: state }), _jsx("small", { style: { color: 'var(--text-secondary)' }, children: label })] }), _jsx("div", { style: { fontSize: 22, fontWeight: 600 }, children: value })] }));
}
function DetailPanel({ detail, loading, onClose, }) {
    const c = detail.connection;
    return (_jsxs("aside", { style: {
            position: 'fixed',
            right: 0,
            top: 0,
            bottom: 0,
            width: 'min(540px, 90vw)',
            background: 'var(--bg-secondary)',
            borderLeft: '1px solid var(--border-color)',
            boxShadow: `0 0 24px var(--shadow)`,
            padding: 24,
            overflow: 'auto',
            zIndex: 100,
            animation: `slide-in var(--motion-med) var(--ease-out)`,
        }, children: [_jsxs("header", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }, children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Connection" }), _jsx("h3", { style: { marginTop: 4, wordBreak: 'break-all' }, children: c.conn_id })] }), _jsx("button", { type: "button", className: "secondary-button", onClick: onClose, children: "Close" })] }), _jsxs("dl", { style: { display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: 13, marginTop: 16 }, children: [_jsx("dt", { children: "Process" }), _jsxs("dd", { children: [c.process_name ?? '—', " \u00B7 pid ", c.pid ?? '—'] }), _jsx("dt", { children: "User" }), _jsx("dd", { children: c.user_name ?? '—' }), _jsx("dt", { children: "Source" }), _jsxs("dd", { children: [c.src_ip, ":", c.src_port] }), _jsx("dt", { children: "Destination" }), _jsxs("dd", { children: [c.dst_ip, ":", c.dst_port, " (", c.protocol ?? 'tcp', ")"] }), _jsx("dt", { children: "Started" }), _jsx("dd", { children: formatTs(c.started_at) }), _jsx("dt", { children: "Ended" }), _jsx("dd", { children: c.ended_at ? formatTs(c.ended_at) : '— still active' }), _jsx("dt", { children: "Duration" }), _jsx("dd", { children: formatDuration(c.duration_ms) }), _jsx("dt", { children: "Bytes in / out" }), _jsxs("dd", { children: [formatBytes(c.bytes_in ?? 0), " / ", _jsx("strong", { children: formatBytes(c.bytes_out ?? 0) })] }), c.threat_match ? (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Threat" }), _jsx("dd", { children: _jsxs(Badge, { variant: "critical", children: [c.threat_feed ?? 'match', " \u00B7 score ", c.threat_score ?? 0] }) })] })) : null, c.bastion_session_id ? (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Bastion session" }), _jsx("dd", { style: { fontFamily: 'ui-monospace, monospace', fontSize: 12 }, children: c.bastion_session_id })] })) : null] }), _jsx("h4", { style: { marginTop: 24 }, children: "Forensic timeline" }), loading ? (_jsx("p", { style: { color: 'var(--text-secondary)' }, children: "Loading correlated events\u2026" })) : (_jsx(EventTimeline, { events: detail.events ?? [] })), _jsx("style", { children: `@keyframes slide-in { from { transform: translateX(100%); } to { transform: translateX(0); } }` })] }));
}
function formatTs(iso) {
    if (!iso)
        return '—';
    try {
        const d = new Date(iso);
        return d.toLocaleString();
    }
    catch {
        return iso;
    }
}
function formatBytes(n) {
    if (!n)
        return '0 B';
    if (n < 1024)
        return `${n} B`;
    if (n < 1024 * 1024)
        return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024)
        return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
function formatDuration(ms) {
    if (!ms)
        return '—';
    if (ms < 1000)
        return `${ms}ms`;
    if (ms < 60_000)
        return `${(ms / 1000).toFixed(1)}s`;
    if (ms < 3_600_000)
        return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
    return `${Math.floor(ms / 3_600_000)}h ${Math.floor((ms % 3_600_000) / 60_000)}m`;
}
