import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { EmptyState } from '../components/EmptyState';
// Custom dashboards builder.
// Layout: left rail = list of user's dashboards + "New". Right pane =
// selected dashboard's widgets in a grid. Each widget renders a small
// preview based on widget_type; clicking opens an edit dialog.
const WIDGET_TYPES = [
    { value: 'db_query', label: 'DB query', description: 'Pull from pg_stat_statements / dm_exec_query_stats — top queries by rows or duration.' },
    { value: 'sys_resources', label: 'System resources', description: 'CPU / memory / disk over time, sourced from agent telemetry.' },
    { value: 'log_size', label: 'Log size', description: 'Total bytes of log lines forwarded from selected nodes.' },
    { value: 'network_bytes', label: 'Network bytes', description: 'Sum of bytes_in / bytes_out from connection events.' },
];
export function Dashboards() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 1, offset: 0 });
    const tenantId = tenants[0]?.id;
    const [dashboards, setDashboards] = useState([]);
    const [selected, setSelected] = useState(null);
    const [error, setError] = useState(null);
    const [creating, setCreating] = useState(false);
    const [newName, setNewName] = useState('');
    const [editingWidget, setEditingWidget] = useState(null);
    const [adding, setAdding] = useState(false);
    const refresh = useCallback(async () => {
        if (!tenantId)
            return;
        try {
            const list = await client.listDashboards(tenantId);
            setDashboards(list);
            if (list.length && !selected) {
                const detail = await client.getDashboard(list[0].id);
                setSelected(detail);
            }
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'load failed');
        }
    }, [client, tenantId, selected]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const handleCreate = async (e) => {
        e.preventDefault();
        if (!tenantId || !newName.trim())
            return;
        try {
            const d = await client.createDashboard({ tenant_id: tenantId, name: newName.trim() });
            setDashboards((cur) => [d, ...cur]);
            setSelected(d);
            setCreating(false);
            setNewName('');
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'create failed');
        }
    };
    const handleSelect = async (d) => {
        try {
            const detail = await client.getDashboard(d.id);
            setSelected(detail);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'load failed');
        }
    };
    const handleAddWidget = async (payload) => {
        if (!selected)
            return;
        try {
            await client.createWidget(selected.id, payload);
            const refreshed = await client.getDashboard(selected.id);
            setSelected(refreshed);
            setAdding(false);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'create widget failed');
        }
    };
    const handleSaveWidget = async (w) => {
        if (!selected)
            return;
        try {
            await client.updateWidget(selected.id, w.id, {
                title: w.title,
                widget_type: w.widget_type,
                spec: w.spec,
                node_ids: w.node_ids,
                refresh_seconds: w.refresh_seconds,
                sort_order: w.sort_order,
            });
            const refreshed = await client.getDashboard(selected.id);
            setSelected(refreshed);
            setEditingWidget(null);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'update widget failed');
        }
    };
    const handleDeleteWidget = async (id) => {
        if (!selected)
            return;
        try {
            await client.deleteWidget(selected.id, id);
            const refreshed = await client.getDashboard(selected.id);
            setSelected(refreshed);
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'delete failed');
        }
    };
    const handleDeleteDashboard = async () => {
        if (!selected)
            return;
        if (!confirm(`Delete dashboard "${selected.name}"? This cannot be undone.`))
            return;
        try {
            await client.deleteDashboard(selected.id);
            setSelected(null);
            await refresh();
        }
        catch (e) {
            setError(e instanceof Error ? e.message : 'delete failed');
        }
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Custom dashboards" }), _jsx("h2", { children: "Build views that pull from the servers + metrics that matter to you" }), _jsx("p", { className: "subtitle", children: "DB queries, system resources, log volume, network bytes \u2014 pick one or more nodes per widget, set a refresh interval, and the data renders live." })] }), _jsx("button", { type: "button", className: "primary-button", onClick: () => setCreating(true), children: "New dashboard" })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, creating ? (_jsxs("form", { onSubmit: handleCreate, style: { display: 'flex', gap: 8, marginBottom: 16 }, children: [_jsx("input", { autoFocus: true, placeholder: "Dashboard name", value: newName, onChange: (e) => setNewName(e.target.value), style: { flex: 1 } }), _jsx("button", { type: "submit", className: "primary-button", children: "Create" }), _jsx("button", { type: "button", className: "secondary-button", onClick: () => setCreating(false), children: "Cancel" })] })) : null, dashboards.length === 0 ? (_jsx(EmptyState, { title: "No dashboards yet", description: "Click New dashboard to author your first one. Add widgets that pull from the nodes + metrics you care about." })) : (_jsxs("div", { style: { display: 'grid', gridTemplateColumns: '240px 1fr', gap: 24 }, children: [_jsx("aside", { children: _jsx("ul", { style: { listStyle: 'none', padding: 0, margin: 0 }, children: dashboards.map((d) => (_jsx("li", { children: _jsx("button", { type: "button", onClick: () => handleSelect(d), className: selected?.id === d.id ? 'primary-button' : 'secondary-button', style: { width: '100%', justifyContent: 'flex-start', marginBottom: 4 }, children: d.name }) }, d.id))) }) }), _jsx("main", { children: selected ? (_jsxs(_Fragment, { children: [_jsxs("header", { style: { display: 'flex', justifyContent: 'space-between', marginBottom: 16 }, children: [_jsxs("div", { children: [_jsx("h3", { children: selected.name }), _jsxs("p", { style: { color: 'var(--text-secondary)', fontSize: 13 }, children: [selected.description || 'No description', " \u00B7 ", selected.widgets?.length ?? 0, " widget(s)", selected.shared ? ' · shared with tenant' : ' · private'] })] }), _jsxs("div", { style: { display: 'flex', gap: 8 }, children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => setAdding(true), children: "Add widget" }), _jsx("button", { type: "button", className: "secondary-button", onClick: handleDeleteDashboard, children: "Delete dashboard" })] })] }), (selected.widgets ?? []).length === 0 ? (_jsx(EmptyState, { title: "No widgets on this dashboard yet", description: "Click Add widget to pull data from one or more nodes." })) : (_jsx("div", { className: "card-grid", style: { gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))' }, children: (selected.widgets ?? []).map((w) => (_jsxs("article", { className: "card", style: { padding: 16 }, children: [_jsxs("header", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }, children: [_jsx("h4", { children: w.title }), _jsx("small", { style: { color: 'var(--text-secondary)' }, children: WIDGET_TYPES.find((t) => t.value === w.widget_type)?.label })] }), _jsxs("p", { style: { fontSize: 13, color: 'var(--text-secondary)' }, children: [w.node_ids.length === 0 ? 'All nodes' : `${w.node_ids.length} node(s)`, " \u00B7 refresh every ", w.refresh_seconds, "s"] }), _jsxs("div", { style: { display: 'flex', gap: 8, marginTop: 12 }, children: [_jsx("button", { type: "button", className: "secondary-button", onClick: () => setEditingWidget(w), children: "Edit" }), _jsx("button", { type: "button", className: "secondary-button", onClick: () => handleDeleteWidget(w.id), children: "Remove" })] })] }, w.id))) })), adding ? (_jsx(WidgetEditor, { onCancel: () => setAdding(false), onSave: handleAddWidget })) : null, editingWidget ? (_jsx(WidgetEditor, { initial: editingWidget, onCancel: () => setEditingWidget(null), onSave: (payload) => handleSaveWidget({ ...editingWidget, ...payload, spec: payload.spec, node_ids: payload.node_ids }) })) : null] })) : (_jsx(EmptyState, { title: "Pick a dashboard", description: "Select one from the left, or create a new one." })) })] }))] }));
}
function WidgetEditor({ initial, onCancel, onSave }) {
    const client = useApiClient();
    const [title, setTitle] = useState(initial?.title ?? '');
    const [widgetType, setWidgetType] = useState(initial?.widget_type ?? 'sys_resources');
    const [refresh, setRefresh] = useState(initial?.refresh_seconds ?? 30);
    const [allNodes, setAllNodes] = useState([]);
    const [selectedNodeIDs, setSelectedNodeIDs] = useState(initial?.node_ids ?? []);
    const [specJson, setSpecJson] = useState(JSON.stringify(initial?.spec ?? defaultSpec(widgetType), null, 2));
    useEffect(() => {
        if (!initial) {
            setSpecJson(JSON.stringify(defaultSpec(widgetType), null, 2));
        }
    }, [widgetType, initial]);
    useEffect(() => {
        client.listNodes({ limit: 200 }).then((r) => setAllNodes(r.data)).catch(() => { });
    }, [client]);
    const submit = (e) => {
        e.preventDefault();
        let spec = {};
        try {
            spec = specJson.trim() ? JSON.parse(specJson) : {};
        }
        catch {
            alert('Spec is not valid JSON');
            return;
        }
        onSave({
            title: title.trim() || 'Untitled',
            widget_type: widgetType,
            spec,
            node_ids: selectedNodeIDs,
            refresh_seconds: Math.max(5, refresh),
            sort_order: initial?.sort_order ?? 0,
        });
    };
    return (_jsxs("aside", { style: {
            position: 'fixed', right: 0, top: 0, bottom: 0,
            width: 'min(560px, 90vw)', zIndex: 100,
            background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border-color)',
            boxShadow: '0 0 24px var(--shadow)', padding: 24, overflow: 'auto',
        }, children: [_jsxs("header", { style: { display: 'flex', justifyContent: 'space-between' }, children: [_jsx("h3", { children: initial ? 'Edit widget' : 'New widget' }), _jsx("button", { type: "button", className: "secondary-button", onClick: onCancel, children: "Close" })] }), _jsxs("form", { onSubmit: submit, style: { display: 'grid', gap: 12, marginTop: 16 }, children: [_jsxs("label", { children: ["Title", _jsx("input", { value: title, onChange: (e) => setTitle(e.target.value), required: true })] }), _jsxs("label", { children: ["Widget type", _jsx("select", { value: widgetType, onChange: (e) => setWidgetType(e.target.value), children: WIDGET_TYPES.map((t) => _jsx("option", { value: t.value, children: t.label }, t.value)) }), _jsx("small", { style: { display: 'block', color: 'var(--text-secondary)', marginTop: 4 }, children: WIDGET_TYPES.find((t) => t.value === widgetType)?.description })] }), _jsxs("label", { children: ["Refresh interval (seconds)", _jsx("input", { type: "number", min: 5, value: refresh, onChange: (e) => setRefresh(Number(e.target.value)) })] }), _jsxs("label", { children: ["Servers (empty = all nodes in tenant)", _jsx("select", { multiple: true, value: selectedNodeIDs, onChange: (e) => setSelectedNodeIDs(Array.from(e.target.selectedOptions).map((o) => o.value)), style: { minHeight: 120 }, children: allNodes.map((n) => (_jsx("option", { value: n.id, children: n.hostname || n.id }, n.id))) })] }), _jsxs("label", { children: ["Spec (JSON \u2014 see widget-type description)", _jsx("textarea", { rows: 8, value: specJson, onChange: (e) => setSpecJson(e.target.value), style: { fontFamily: 'ui-monospace, monospace', fontSize: 12 } })] }), _jsxs("div", { style: { display: 'flex', gap: 8 }, children: [_jsx("button", { type: "submit", className: "primary-button", children: initial ? 'Save' : 'Create' }), _jsx("button", { type: "button", className: "secondary-button", onClick: onCancel, children: "Cancel" })] })] })] }));
}
function defaultSpec(t) {
    switch (t) {
        case 'db_query':
            return { engine: 'postgres', target_name: '', limit: 10, order_by: 'rows' };
        case 'sys_resources':
            return { metric: 'cpu', range: '1h' };
        case 'log_size':
            return { range: '24h', source_program: '' };
        case 'network_bytes':
            return { direction: 'both', range: '1h' };
    }
}
