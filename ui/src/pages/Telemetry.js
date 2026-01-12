import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useTelemetryMetrics, useTelemetryLogs } from '../hooks/useTelemetry';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import './Telemetry.css';
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
function getLogLevelColor(level) {
    switch (level.toLowerCase()) {
        case 'error':
        case 'critical':
            return '#dc2626';
        case 'warn':
        case 'warning':
            return '#f59e0b';
        case 'info':
            return '#3b82f6';
        case 'debug':
            return '#6b7280';
        default:
            return '#6b7280';
    }
}
export function Telemetry() {
    const [viewMode, setViewMode] = useState('metrics');
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const [selectedNode, setSelectedNode] = useState(undefined);
    const [metricNameFilter, setMetricNameFilter] = useState('');
    const [logLevelFilter, setLogLevelFilter] = useState('');
    const [logSourceFilter, setLogSourceFilter] = useState('');
    const [limit] = useState(100);
    const [offset, setOffset] = useState(0);
    const { data: tenants } = useTenants();
    const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });
    const { data: metrics, loading: metricsLoading, error: metricsError, pagination: metricsPagination, reload: reloadMetrics, } = useTelemetryMetrics({
        tenant_id: selectedTenant,
        node_id: selectedNode,
        metric_name: metricNameFilter || undefined,
        limit,
        offset: viewMode === 'metrics' ? offset : 0,
    });
    const { data: logs, loading: logsLoading, error: logsError, pagination: logsPagination, reload: reloadLogs, } = useTelemetryLogs({
        tenant_id: selectedTenant,
        node_id: selectedNode,
        log_level: logLevelFilter || undefined,
        log_source: logSourceFilter || undefined,
        limit,
        offset: viewMode === 'logs' ? offset : 0,
    });
    const uniqueMetricNames = useMemo(() => {
        const names = new Set();
        metrics.forEach((m) => names.add(m.metric_name));
        return Array.from(names).sort();
    }, [metrics]);
    const uniqueLogLevels = useMemo(() => {
        const levels = new Set();
        logs.forEach((l) => levels.add(l.log_level));
        return Array.from(levels).sort();
    }, [logs]);
    const uniqueLogSources = useMemo(() => {
        const sources = new Set();
        logs.forEach((l) => {
            if (l.log_source)
                sources.add(l.log_source);
        });
        return Array.from(sources).sort();
    }, [logs]);
    const handleRefresh = () => {
        if (viewMode === 'metrics') {
            reloadMetrics();
        }
        else {
            reloadLogs();
        }
    };
    const currentPagination = viewMode === 'metrics' ? metricsPagination : logsPagination;
    const currentLoading = viewMode === 'metrics' ? metricsLoading : logsLoading;
    const currentError = viewMode === 'metrics' ? metricsError : logsError;
    return (_jsxs("div", { className: "telemetry-page", children: [_jsxs("div", { className: "page-header", children: [_jsxs("div", { children: [_jsx("h1", { children: "Telemetry Dashboard" }), _jsx("p", { className: "subtitle", children: "Monitor metrics and logs from your infrastructure" })] }), _jsxs("div", { className: "page-actions", children: [_jsxs("div", { className: "view-mode-toggle", children: [_jsx("button", { type: "button", className: viewMode === 'metrics' ? 'btn-primary' : 'btn-secondary', onClick: () => {
                                            setViewMode('metrics');
                                            setOffset(0);
                                        }, children: "Metrics" }), _jsx("button", { type: "button", className: viewMode === 'logs' ? 'btn-primary' : 'btn-secondary', onClick: () => {
                                            setViewMode('logs');
                                            setOffset(0);
                                        }, children: "Logs" })] }), _jsx("button", { type: "button", onClick: handleRefresh, className: "btn-secondary", children: "Refresh" })] })] }), _jsxs("div", { className: "filters-section", children: [_jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Tenant" }), _jsxs("select", { id: "tenant-filter", value: selectedTenant || '', onChange: (e) => {
                                    setSelectedTenant(e.target.value || undefined);
                                    setSelectedNode(undefined);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Tenants" }), tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "node-filter", children: "Node" }), _jsxs("select", { id: "node-filter", value: selectedNode || '', onChange: (e) => {
                                    setSelectedNode(e.target.value || undefined);
                                    setOffset(0);
                                }, disabled: !selectedTenant, children: [_jsx("option", { value: "", children: "All Nodes" }), nodes.map((n) => (_jsx("option", { value: n.id, children: n.hostname }, n.id)))] })] }), viewMode === 'metrics' && (_jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "metric-name-filter", children: "Metric Name" }), _jsxs("select", { id: "metric-name-filter", value: metricNameFilter, onChange: (e) => {
                                    setMetricNameFilter(e.target.value);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Metrics" }), uniqueMetricNames.map((name) => (_jsx("option", { value: name, children: name }, name)))] })] })), viewMode === 'logs' && (_jsxs(_Fragment, { children: [_jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "log-level-filter", children: "Log Level" }), _jsxs("select", { id: "log-level-filter", value: logLevelFilter, onChange: (e) => {
                                            setLogLevelFilter(e.target.value);
                                            setOffset(0);
                                        }, children: [_jsx("option", { value: "", children: "All Levels" }), uniqueLogLevels.map((level) => (_jsx("option", { value: level, children: level.toUpperCase() }, level)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "log-source-filter", children: "Log Source" }), _jsxs("select", { id: "log-source-filter", value: logSourceFilter, onChange: (e) => {
                                            setLogSourceFilter(e.target.value);
                                            setOffset(0);
                                        }, children: [_jsx("option", { value: "", children: "All Sources" }), uniqueLogSources.map((source) => (_jsx("option", { value: source, children: source }, source)))] })] })] }))] }), currentError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading telemetry data: ", currentError] }) })), viewMode === 'metrics' ? (_jsxs("div", { className: "telemetry-content", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Metrics" }), _jsxs("div", { className: "results-count", children: ["Showing ", metrics.length, " of ", currentPagination.total] })] }), currentLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading metrics..." })) : metrics.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No metrics found matching your filters." }) })) : (_jsxs(_Fragment, { children: [_jsx("div", { className: "table-container", children: _jsxs("table", { className: "telemetry-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Timestamp" }), _jsx("th", { children: "Metric Name" }), _jsx("th", { children: "Value" }), _jsx("th", { children: "Unit" }), _jsx("th", { children: "Node" }), _jsx("th", { children: "Labels" })] }) }), _jsx("tbody", { children: metrics.map((metric) => {
                                                const node = nodes.find((n) => n.id === metric.node_id);
                                                return (_jsxs("tr", { children: [_jsx("td", { children: formatDate(metric.timestamp) }), _jsx("td", { children: _jsx("code", { children: metric.metric_name }) }), _jsx("td", { className: "metric-value", children: metric.metric_value.toLocaleString() }), _jsx("td", { children: metric.metric_unit || '—' }), _jsx("td", { children: node?.hostname || metric.node_id || '—' }), _jsx("td", { children: metric.labels && Object.keys(metric.labels).length > 0 ? (_jsxs("details", { children: [_jsx("summary", { children: "View labels" }), _jsx("pre", { children: JSON.stringify(metric.labels, null, 2) })] })) : ('—') })] }, metric.id));
                                            }) })] }) }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || currentLoading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(currentPagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= currentPagination.total || currentLoading, className: "btn-secondary", children: "Next" })] })] }))] })) : (_jsxs("div", { className: "telemetry-content", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Logs" }), _jsxs("div", { className: "results-count", children: ["Showing ", logs.length, " of ", currentPagination.total] })] }), currentLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading logs..." })) : logs.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No logs found matching your filters." }) })) : (_jsxs(_Fragment, { children: [_jsx("div", { className: "logs-container", children: logs.map((log) => {
                                    const node = nodes.find((n) => n.id === log.node_id);
                                    return (_jsxs("div", { className: "log-entry", children: [_jsxs("div", { className: "log-header", children: [_jsx("span", { className: "log-level-badge", style: { backgroundColor: getLogLevelColor(log.log_level) }, children: log.log_level.toUpperCase() }), _jsx("span", { className: "log-timestamp", children: formatDate(log.timestamp) }), log.log_source && _jsx("span", { className: "log-source", children: log.log_source }), log.log_program && _jsx("span", { className: "log-program", children: log.log_program }), node && _jsx("span", { className: "log-node", children: node.hostname })] }), _jsx("div", { className: "log-message", children: log.log_message }), log.labels && Object.keys(log.labels).length > 0 && (_jsxs("details", { className: "log-labels", children: [_jsx("summary", { children: "Labels" }), _jsx("pre", { children: JSON.stringify(log.labels, null, 2) })] }))] }, log.id));
                                }) }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || currentLoading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(currentPagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= currentPagination.total || currentLoading, className: "btn-secondary", children: "Next" })] })] }))] }))] }));
}
