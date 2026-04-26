import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useAuditLogs } from '../hooks/useAuditLogs';
import { useTenants } from '../hooks/useTenants';
import './Audit.css';
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
function formatRelativeTime(value) {
    if (!value) {
        return '—';
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return value;
    }
    const now = new Date();
    const diffMs = now.getTime() - parsed.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);
    if (diffMins < 1)
        return 'Just now';
    if (diffMins < 60)
        return `${diffMins}m ago`;
    if (diffHours < 24)
        return `${diffHours}h ago`;
    if (diffDays < 7)
        return `${diffDays}d ago`;
    return parsed.toLocaleDateString();
}
function getActionColor(action) {
    if (action.includes('.create') || action.includes('.created'))
        return 'var(--state-healthy)';
    if (action.includes('.update') || action.includes('.updated'))
        return 'var(--state-info)';
    if (action.includes('.delete') || action.includes('.deleted'))
        return 'var(--state-critical)';
    if (action.includes('.failed') || action.includes('.error'))
        return 'var(--state-critical)';
    if (action.includes('.success') || action.includes('.succeeded'))
        return 'var(--state-healthy)';
    return 'var(--text-secondary)';
}
function exportToCSV(logs) {
    const headers = ['Timestamp', 'Actor Type', 'Action', 'Resource Type', 'Resource ID', 'Tenant ID', 'Metadata'];
    const rows = logs.map((log) => [
        log.created_at,
        log.actor_type,
        log.action,
        log.resource_type,
        log.resource_id || '',
        log.tenant_id || '',
        JSON.stringify(log.metadata || {}),
    ]);
    const csv = [headers.join(','), ...rows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `audit-logs-${new Date().toISOString().split('T')[0]}.csv`;
    a.click();
    URL.revokeObjectURL(url);
}
export function Audit() {
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const [actorTypeFilter, setActorTypeFilter] = useState('');
    const [actionFilter, setActionFilter] = useState('');
    const [resourceTypeFilter, setResourceTypeFilter] = useState('');
    const [viewMode, setViewMode] = useState('table');
    const [limit] = useState(100);
    const [offset, setOffset] = useState(0);
    const { data: tenants } = useTenants();
    const { data: logs, loading, error, pagination, reload, } = useAuditLogs({
        tenant_id: selectedTenant,
        actor_type: actorTypeFilter || undefined,
        action: actionFilter || undefined,
        resource_type: resourceTypeFilter || undefined,
        limit,
        offset,
    });
    const uniqueActions = useMemo(() => {
        const actions = new Set();
        logs.forEach((log) => actions.add(log.action));
        return Array.from(actions).sort();
    }, [logs]);
    const uniqueResourceTypes = useMemo(() => {
        const types = new Set();
        logs.forEach((log) => types.add(log.resource_type));
        return Array.from(types).sort();
    }, [logs]);
    const handleExport = () => {
        if (logs.length === 0)
            return;
        exportToCSV(logs);
    };
    const handleRefresh = () => {
        reload();
    };
    return (_jsxs("div", { className: "audit-page", children: [_jsxs("div", { className: "page-header", children: [_jsxs("div", { children: [_jsx("h1", { children: "Audit trail" }), _jsx("p", { className: "subtitle", children: "Who did what, when. Full record for SOC 2, ISO 27001, and incident review." })] }), _jsxs("div", { className: "page-actions", children: [_jsx("button", { type: "button", onClick: handleRefresh, className: "btn-secondary", children: "Refresh" }), _jsx("button", { type: "button", onClick: handleExport, className: "btn-primary", disabled: logs.length === 0, children: "Export CSV" })] })] }), _jsxs("div", { className: "filters-section", children: [_jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Tenant" }), _jsxs("select", { id: "tenant-filter", value: selectedTenant || '', onChange: (e) => {
                                    setSelectedTenant(e.target.value || undefined);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Tenants" }), tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "actor-type-filter", children: "Actor Type" }), _jsxs("select", { id: "actor-type-filter", value: actorTypeFilter, onChange: (e) => {
                                    setActorTypeFilter(e.target.value);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Types" }), _jsx("option", { value: "user", children: "User" }), _jsx("option", { value: "system", children: "System" })] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "action-filter", children: "Action" }), _jsxs("select", { id: "action-filter", value: actionFilter, onChange: (e) => {
                                    setActionFilter(e.target.value);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Actions" }), uniqueActions.map((action) => (_jsx("option", { value: action, children: action }, action)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "resource-type-filter", children: "Resource Type" }), _jsxs("select", { id: "resource-type-filter", value: resourceTypeFilter, onChange: (e) => {
                                    setResourceTypeFilter(e.target.value);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Resources" }), uniqueResourceTypes.map((type) => (_jsx("option", { value: type, children: type }, type)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "view-mode-filter", children: "View" }), _jsxs("select", { id: "view-mode-filter", value: viewMode, onChange: (e) => setViewMode(e.target.value), children: [_jsx("option", { value: "table", children: "Table" }), _jsx("option", { value: "timeline", children: "Timeline" })] })] })] }), error && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading audit logs: ", error] }) })), _jsxs("div", { className: "audit-stats", children: [_jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: pagination.total }), _jsx("div", { className: "stat-label", children: "Total Events" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: logs.filter((l) => l.actor_type === 'user').length }), _jsx("div", { className: "stat-label", children: "User Actions" })] }), _jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: logs.filter((l) => l.actor_type === 'system').length }), _jsx("div", { className: "stat-label", children: "System Events" })] })] }), loading ? (_jsx("div", { className: "loading-placeholder", children: "Loading audit logs..." })) : logs.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No audit logs found matching your filters." }) })) : viewMode === 'table' ? (_jsx(_Fragment, { children: _jsxs("div", { className: "results-section", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Audit Log Entries" }), _jsxs("div", { className: "results-count", children: ["Showing ", logs.length, " of ", pagination.total] })] }), _jsx("div", { className: "table-container", children: _jsxs("table", { className: "audit-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Timestamp" }), _jsx("th", { children: "Actor" }), _jsx("th", { children: "Action" }), _jsx("th", { children: "Resource" }), _jsx("th", { children: "Details" })] }) }), _jsx("tbody", { children: logs.map((log) => (_jsxs("tr", { children: [_jsxs("td", { className: "timestamp-cell", children: [_jsx("div", { className: "timestamp-primary", children: formatDate(log.created_at) }), _jsx("div", { className: "timestamp-secondary", children: formatRelativeTime(log.created_at) })] }), _jsxs("td", { children: [_jsx("span", { className: "actor-badge", children: log.actor_type }), log.actor_id && _jsxs("span", { className: "actor-id", children: ["(", log.actor_id.slice(0, 8), "...)"] })] }), _jsx("td", { children: _jsx("span", { className: "action-badge", style: { backgroundColor: getActionColor(log.action) }, children: log.action }) }), _jsx("td", { children: _jsxs("div", { className: "resource-info", children: [_jsx("span", { className: "resource-type", children: log.resource_type }), log.resource_id && (_jsx("code", { className: "resource-id", title: log.resource_id, children: log.resource_id.length > 20 ? `${log.resource_id.slice(0, 20)}...` : log.resource_id }))] }) }), _jsx("td", { className: "details-cell", children: log.metadata && Object.keys(log.metadata).length > 0 ? (_jsxs("details", { children: [_jsx("summary", { children: "View metadata" }), _jsx("pre", { children: JSON.stringify(log.metadata, null, 2) })] })) : ('—') })] }, log.id))) })] }) }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || loading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(pagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= pagination.total || loading, className: "btn-secondary", children: "Next" })] })] }) })) : (_jsxs("div", { className: "timeline-section", children: [_jsx("h2", { children: "Timeline View" }), _jsx("div", { className: "timeline", children: logs.map((log) => (_jsxs("div", { className: "timeline-item", children: [_jsx("div", { className: "timeline-marker", style: { backgroundColor: getActionColor(log.action) } }), _jsxs("div", { className: "timeline-content", children: [_jsxs("div", { className: "timeline-header", children: [_jsx("span", { className: "timeline-action", style: { color: getActionColor(log.action) }, children: log.action }), _jsx("span", { className: "timeline-time", children: formatRelativeTime(log.created_at) })] }), _jsxs("div", { className: "timeline-body", children: [_jsxs("div", { className: "timeline-details", children: [_jsx("span", { className: "detail-label", children: "Actor:" }), _jsx("span", { className: "detail-value", children: log.actor_type }), log.actor_id && _jsxs("span", { className: "detail-value secondary", children: ["(", log.actor_id.slice(0, 8), "...)"] })] }), _jsxs("div", { className: "timeline-details", children: [_jsx("span", { className: "detail-label", children: "Resource:" }), _jsx("span", { className: "detail-value", children: log.resource_type }), log.resource_id && (_jsxs("code", { className: "detail-value secondary", children: [log.resource_id.slice(0, 20), "..."] }))] }), log.metadata && Object.keys(log.metadata).length > 0 && (_jsxs("details", { className: "timeline-metadata", children: [_jsx("summary", { children: "Metadata" }), _jsx("pre", { children: JSON.stringify(log.metadata, null, 2) })] }))] }), _jsx("div", { className: "timeline-footer", children: _jsx("span", { className: "timeline-timestamp", children: formatDate(log.created_at) }) })] })] }, log.id))) })] }))] }));
}
