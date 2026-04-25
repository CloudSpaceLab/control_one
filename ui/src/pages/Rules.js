import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useState, lazy, Suspense } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useEventStream } from '../hooks/useEventStream';
// Lazy-load the visual builder so its drag/drop state machine doesn't slow
// the initial page render for operators who only want to author rules in the
// flat form.
const RuleBuilder = lazy(() => import('./RuleBuilder').then((m) => ({ default: m.RuleBuilder })));
export function Rules() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tab, setTab] = useState('port');
    const [tenantId, setTenantId] = useState('');
    const [portRules, setPortRules] = useState([]);
    const [logRules, setLogRules] = useState([]);
    const [error, setError] = useState(null);
    const [notice, setNotice] = useState(null);
    useEffect(() => {
        if (!tenantId && tenants[0]?.id)
            setTenantId(tenants[0].id);
    }, [tenants, tenantId]);
    const refresh = useCallback(async () => {
        if (!tenantId)
            return;
        try {
            const [p, l] = await Promise.all([
                client.listPortRules({ tenantId, limit: 100, offset: 0 }),
                client.listLogRules({ tenantId, limit: 100, offset: 0 }),
            ]);
            setPortRules(p.data);
            setLogRules(l.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
    }, [client, tenantId]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    useEventStream(tenantId, ['policy.updated', 'rule.triggered'], (ev) => {
        setNotice(`Realtime: ${ev.topic}`);
        refresh();
        window.setTimeout(() => setNotice(null), 3000);
    });
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Detection" }), _jsx("h2", { children: "Detection rules" }), _jsx("p", { className: "subtitle", children: "Define what's allowed. Detect violations instantly. Real-time enforcement on every node." })] }), _jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", style: { padding: '0.4rem' }, children: tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id))) })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, notice ? _jsx("p", { className: "muted", children: notice }) : null, _jsxs("div", { className: "tab-row", role: "tablist", style: { display: 'flex', gap: '0.5rem', marginBottom: '1rem' }, children: [_jsxs("button", { type: "button", role: "tab", "aria-selected": tab === 'port', className: tab === 'port' ? 'primary-button' : 'secondary-button', onClick: () => setTab('port'), children: ["Port rules (", portRules.length, ")"] }), _jsxs("button", { type: "button", role: "tab", "aria-selected": tab === 'log', className: tab === 'log' ? 'primary-button' : 'secondary-button', onClick: () => setTab('log'), children: ["Log rules (", logRules.length, ")"] }), _jsx("button", { type: "button", role: "tab", "aria-selected": tab === 'builder', className: tab === 'builder' ? 'primary-button' : 'secondary-button', onClick: () => setTab('builder'), title: "Compose rules visually with drag-and-drop blocks", children: "Visual builder" })] }), tab === 'port' ? (_jsx(PortRulesPane, { tenantId: tenantId, rules: portRules, onRefresh: refresh })) : tab === 'log' ? (_jsx(LogRulesPane, { tenantId: tenantId, rules: logRules, onRefresh: refresh })) : (_jsx(Suspense, { fallback: _jsx("p", { className: "muted", children: "Loading builder\u2026" }), children: _jsx(RuleBuilder, {}) }))] }));
}
function PortRulesPane({ tenantId, rules, onRefresh, }) {
    const client = useApiClient();
    const [form, setForm] = useState({
        tenant_id: tenantId,
        name: '',
        port: 22,
        protocol: 'tcp',
        expected_state: 'closed',
        severity: 'medium',
        action: 'notify',
        enabled: true,
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
            await client.createPortRule(form);
            setForm({ ...form, name: '' });
            onRefresh();
        }
        finally {
            setSubmitting(false);
        }
    };
    const remove = async (id) => {
        if (!window.confirm('Delete this port rule?'))
            return;
        await client.deletePortRule(id);
        onRefresh();
    };
    return (_jsxs("div", { children: [_jsxs("form", { className: "form-row", onSubmit: submit, style: { display: 'grid', gridTemplateColumns: 'repeat(6, 1fr)', gap: '0.5rem', alignItems: 'end' }, children: [_jsxs("label", { children: ["Name", _jsx("input", { required: true, value: form.name, onChange: (e) => setForm({ ...form, name: e.target.value }) })] }), _jsxs("label", { children: ["Port", _jsx("input", { type: "number", min: 1, max: 65535, required: true, value: form.port, onChange: (e) => setForm({ ...form, port: Number(e.target.value) }) })] }), _jsxs("label", { children: ["Protocol", _jsxs("select", { value: form.protocol, onChange: (e) => setForm({ ...form, protocol: e.target.value }), children: [_jsx("option", { value: "tcp", children: "tcp" }), _jsx("option", { value: "udp", children: "udp" })] })] }), _jsxs("label", { children: ["Expected", _jsxs("select", { value: form.expected_state, onChange: (e) => setForm({ ...form, expected_state: e.target.value }), children: [_jsx("option", { value: "closed", children: "closed" }), _jsx("option", { value: "open", children: "open" })] })] }), _jsxs("label", { children: ["Severity", _jsxs("select", { value: form.severity, onChange: (e) => setForm({ ...form, severity: e.target.value }), children: [_jsx("option", { value: "low", children: "low" }), _jsx("option", { value: "medium", children: "medium" }), _jsx("option", { value: "high", children: "high" }), _jsx("option", { value: "critical", children: "critical" })] })] }), _jsx("button", { type: "submit", className: "primary-button", disabled: submitting, children: "Add rule" })] }), _jsxs("table", { className: "data-table", style: { marginTop: '1rem', width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Port" }), _jsx("th", { children: "Proto" }), _jsx("th", { children: "Expected" }), _jsx("th", { children: "Severity" }), _jsx("th", { children: "Enabled" }), _jsx("th", {})] }) }), _jsx("tbody", { children: rules.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 7, className: "muted", children: "No port rules yet." }) })) : (rules.map((r) => (_jsxs("tr", { children: [_jsx("td", { children: r.name }), _jsx("td", { children: r.port }), _jsx("td", { children: r.protocol }), _jsx("td", { children: r.expected_state }), _jsx("td", { children: r.severity }), _jsx("td", { children: r.enabled ? 'yes' : 'no' }), _jsx("td", { children: _jsx("button", { type: "button", className: "secondary-button", onClick: () => remove(r.id), children: "Delete" }) })] }, r.id)))) })] })] }));
}
function LogRulesPane({ tenantId, rules, onRefresh, }) {
    const client = useApiClient();
    const [form, setForm] = useState({
        tenant_id: tenantId,
        name: '',
        log_source: 'auth',
        pattern: '',
        severity: 'high',
        window_seconds: 60,
        threshold: 3,
        action: 'notify',
        enabled: true,
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
            await client.createLogRule(form);
            setForm({ ...form, name: '', pattern: '' });
            onRefresh();
        }
        finally {
            setSubmitting(false);
        }
    };
    const remove = async (id) => {
        if (!window.confirm('Delete this log rule?'))
            return;
        await client.deleteLogRule(id);
        onRefresh();
    };
    return (_jsxs("div", { children: [_jsxs("form", { className: "form-row", onSubmit: submit, style: { display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: '0.5rem', alignItems: 'end' }, children: [_jsxs("label", { children: ["Name", _jsx("input", { required: true, value: form.name, onChange: (e) => setForm({ ...form, name: e.target.value }) })] }), _jsxs("label", { children: ["Source", _jsx("input", { required: true, value: form.log_source, onChange: (e) => setForm({ ...form, log_source: e.target.value }) })] }), _jsxs("label", { style: { gridColumn: 'span 2' }, children: ["Pattern (regex)", _jsx("input", { required: true, value: form.pattern, onChange: (e) => setForm({ ...form, pattern: e.target.value }) })] }), _jsxs("label", { children: ["Window (s)", _jsx("input", { type: "number", min: 1, value: form.window_seconds, onChange: (e) => setForm({ ...form, window_seconds: Number(e.target.value) }) })] }), _jsxs("label", { children: ["Threshold", _jsx("input", { type: "number", min: 1, value: form.threshold, onChange: (e) => setForm({ ...form, threshold: Number(e.target.value) }) })] }), _jsx("button", { type: "submit", className: "primary-button", disabled: submitting, children: "Add rule" })] }), _jsxs("table", { className: "data-table", style: { marginTop: '1rem', width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Source" }), _jsx("th", { children: "Pattern" }), _jsx("th", { children: "Win" }), _jsx("th", { children: "Thresh" }), _jsx("th", { children: "Sev" }), _jsx("th", {})] }) }), _jsx("tbody", { children: rules.length === 0 ? (_jsx("tr", { children: _jsx("td", { colSpan: 7, className: "muted", children: "No log rules yet." }) })) : (rules.map((r) => (_jsxs("tr", { children: [_jsx("td", { children: r.name }), _jsx("td", { children: r.log_source }), _jsx("td", { children: _jsx("code", { children: r.pattern }) }), _jsxs("td", { children: [r.window_seconds, "s"] }), _jsx("td", { children: r.threshold }), _jsx("td", { children: r.severity }), _jsx("td", { children: _jsx("button", { type: "button", className: "secondary-button", onClick: () => remove(r.id), children: "Delete" }) })] }, r.id)))) })] })] }));
}
