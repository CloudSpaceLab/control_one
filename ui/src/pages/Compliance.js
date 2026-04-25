import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useComplianceResults, useComplianceSummary, useComplianceTrends } from '../hooks/useCompliance';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import './Compliance.css';
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
function getSeverityColor(severity) {
    switch (severity?.toLowerCase()) {
        case 'critical':
            return '#dc2626';
        case 'high':
            return '#ea580c';
        case 'medium':
            return '#f59e0b';
        case 'low':
            return '#84cc16';
        default:
            return '#6b7280';
    }
}
function exportToCSV(results) {
    const headers = ['ID', 'Rule ID', 'Node ID', 'Passed', 'Severity', 'Checked At', 'Details'];
    const rows = results.map((r) => [
        r.id,
        r.rule_id,
        r.node_id || '',
        r.passed ? 'Yes' : 'No',
        r.severity || '',
        r.checked_at || '',
        r.details || '',
    ]);
    const csv = [headers.join(','), ...rows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `compliance-results-${new Date().toISOString().split('T')[0]}.csv`;
    a.click();
    URL.revokeObjectURL(url);
}
export function Compliance() {
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const [selectedNode, setSelectedNode] = useState(undefined);
    const [severityFilter, setSeverityFilter] = useState('');
    const [passedFilter, setPassedFilter] = useState(undefined);
    const [limit] = useState(50);
    const [offset, setOffset] = useState(0);
    const { data: tenants } = useTenants();
    const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });
    const { data: summary, loading: summaryLoading, error: summaryError, reload: reloadSummary, } = useComplianceSummary({
        tenant_id: selectedTenant,
        node_id: selectedNode,
    });
    const { data: trends, loading: trendsLoading,
    // error intentionally unused — trends errors handled by loading state
     } = useComplianceTrends({
        tenant_id: selectedTenant,
        node_id: selectedNode,
        days: 30,
    });
    const { data: results, loading: resultsLoading, error: resultsError, pagination, reload: reloadResults, } = useComplianceResults({
        tenant_id: selectedTenant,
        node_id: selectedNode,
        severity: severityFilter || undefined,
        passed: passedFilter,
        limit,
        offset,
    });
    const complianceScore = useMemo(() => {
        if (!summary || summary.total === 0)
            return null;
        return Math.round((summary.passed / summary.total) * 100);
    }, [summary]);
    const severityBreakdown = useMemo(() => {
        if (!summary)
            return [];
        return Object.entries(summary.by_severity || {})
            .map(([severity, count]) => ({ severity, count }))
            .sort((a, b) => {
            const order = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };
            return (order[a.severity.toLowerCase()] ?? 99) - (order[b.severity.toLowerCase()] ?? 99);
        });
    }, [summary]);
    const handleExport = () => {
        if (results.length === 0)
            return;
        exportToCSV(results);
    };
    const handleRefresh = () => {
        reloadSummary();
        reloadResults();
    };
    return (_jsxs("div", { className: "compliance-page", children: [_jsxs("div", { className: "page-header", children: [_jsxs("div", { children: [_jsx("h1", { children: "Compliance posture" }), _jsx("p", { className: "subtitle", children: "Find violations, fix them, prove continuous control." })] }), _jsxs("div", { className: "page-actions", children: [_jsx("button", { type: "button", onClick: handleRefresh, className: "btn-secondary", children: "Refresh" }), _jsx("button", { type: "button", onClick: handleExport, className: "btn-primary", disabled: results.length === 0, children: "Export CSV" })] })] }), _jsxs("div", { className: "filters-section", children: [_jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Tenant" }), _jsxs("select", { id: "tenant-filter", value: selectedTenant || '', onChange: (e) => {
                                    setSelectedTenant(e.target.value || undefined);
                                    setSelectedNode(undefined);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Tenants" }), tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "node-filter", children: "Node" }), _jsxs("select", { id: "node-filter", value: selectedNode || '', onChange: (e) => {
                                    setSelectedNode(e.target.value || undefined);
                                    setOffset(0);
                                }, disabled: !selectedTenant, children: [_jsx("option", { value: "", children: "All Nodes" }), nodes.map((n) => (_jsx("option", { value: n.id, children: n.hostname }, n.id)))] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "severity-filter", children: "Severity" }), _jsxs("select", { id: "severity-filter", value: severityFilter, onChange: (e) => {
                                    setSeverityFilter(e.target.value);
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All Severities" }), _jsx("option", { value: "critical", children: "Critical" }), _jsx("option", { value: "high", children: "High" }), _jsx("option", { value: "medium", children: "Medium" }), _jsx("option", { value: "low", children: "Low" })] })] }), _jsxs("div", { className: "filter-group", children: [_jsx("label", { htmlFor: "passed-filter", children: "Status" }), _jsxs("select", { id: "passed-filter", value: passedFilter === undefined ? '' : passedFilter.toString(), onChange: (e) => {
                                    const value = e.target.value;
                                    setPassedFilter(value === '' ? undefined : value === 'true');
                                    setOffset(0);
                                }, children: [_jsx("option", { value: "", children: "All" }), _jsx("option", { value: "true", children: "Passed" }), _jsx("option", { value: "false", children: "Failed" })] })] })] }), summaryError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading compliance summary: ", summaryError] }) })), resultsError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading compliance results: ", resultsError] }) })), _jsxs("div", { className: "compliance-overview", children: [_jsxs("div", { className: "score-card", children: [_jsx("div", { className: "score-value", children: summaryLoading ? (_jsx("span", { className: "loading", children: "\u2014" })) : complianceScore !== null ? (_jsxs(_Fragment, { children: [_jsx("span", { className: "score-number", children: complianceScore }), _jsx("span", { className: "score-unit", children: "%" })] })) : ('—') }), _jsx("div", { className: "score-label", children: "Compliance Score" }), summary && (_jsxs("div", { className: "score-details", children: [summary.passed, " of ", summary.total, " checks passed"] }))] }), _jsxs("div", { className: "stats-grid", children: [_jsxs("div", { className: "stat-card", children: [_jsx("div", { className: "stat-value", children: summaryLoading ? '—' : summary?.total || 0 }), _jsx("div", { className: "stat-label", children: "Total Checks" })] }), _jsxs("div", { className: "stat-card success", children: [_jsx("div", { className: "stat-value", children: summaryLoading ? '—' : summary?.passed || 0 }), _jsx("div", { className: "stat-label", children: "Passed" })] }), _jsxs("div", { className: "stat-card error", children: [_jsx("div", { className: "stat-value", children: summaryLoading ? '—' : summary?.failed || 0 }), _jsx("div", { className: "stat-label", children: "Failed" })] })] })] }), severityBreakdown.length > 0 && (_jsxs("div", { className: "severity-breakdown", children: [_jsx("h2", { children: "Violations by Severity" }), _jsx("div", { className: "severity-list", children: severityBreakdown.map(({ severity, count }) => (_jsxs("div", { className: "severity-item", children: [_jsx("div", { className: "severity-indicator", style: { backgroundColor: getSeverityColor(severity) } }), _jsx("span", { className: "severity-name", children: severity.toUpperCase() }), _jsx("span", { className: "severity-count", children: count })] }, severity))) })] })), trends.length > 0 && (_jsxs("div", { className: "trends-section", children: [_jsx("h2", { children: "Compliance Trends (Last 30 Days)" }), _jsx("div", { className: "trends-chart", children: trendsLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading trends..." })) : (_jsx("div", { className: "trends-bars", children: trends.map((trend, idx) => {
                                const maxValue = Math.max(...trends.map((t) => t.total));
                                const passedPercent = maxValue > 0 ? (trend.passed / maxValue) * 100 : 0;
                                const failedPercent = maxValue > 0 ? (trend.failed / maxValue) * 100 : 0;
                                const date = new Date(trend.date);
                                return (_jsxs("div", { className: "trend-bar-group", children: [_jsxs("div", { className: "trend-bar-container", children: [_jsx("div", { className: "trend-bar passed", style: { height: `${passedPercent}%` }, title: `Passed: ${trend.passed}` }), _jsx("div", { className: "trend-bar failed", style: { height: `${failedPercent}%` }, title: `Failed: ${trend.failed}` })] }), _jsx("div", { className: "trend-label", children: date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }) })] }, idx));
                            }) })) })] })), _jsxs("div", { className: "results-section", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Compliance Results" }), _jsxs("div", { className: "results-count", children: ["Showing ", results.length, " of ", pagination.total] })] }), resultsLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading compliance results..." })) : results.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No compliance results found matching your filters." }) })) : (_jsxs(_Fragment, { children: [_jsx("div", { className: "results-table-container", children: _jsxs("table", { className: "results-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Rule ID" }), _jsx("th", { children: "Node" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Severity" }), _jsx("th", { children: "Checked At" }), _jsx("th", { children: "Details" })] }) }), _jsx("tbody", { children: results.map((result) => {
                                                const node = nodes.find((n) => n.id === result.node_id);
                                                return (_jsxs("tr", { className: result.passed ? 'passed' : 'failed', children: [_jsx("td", { children: _jsx("code", { children: result.rule_id }) }), _jsx("td", { children: node?.hostname || result.node_id || '—' }), _jsx("td", { children: _jsx("span", { className: `status-badge ${result.passed ? 'success' : 'error'}`, children: result.passed ? 'Passed' : 'Failed' }) }), _jsx("td", { children: result.severity && (_jsx("span", { className: "severity-badge", style: {
                                                                    backgroundColor: getSeverityColor(result.severity),
                                                                    color: '#fff',
                                                                    padding: '2px 8px',
                                                                    borderRadius: '4px',
                                                                    fontSize: '0.875rem',
                                                                }, children: result.severity.toUpperCase() })) }), _jsx("td", { children: formatDate(result.checked_at) }), _jsx("td", { className: "details-cell", children: result.details ? (_jsxs("details", { children: [_jsx("summary", { children: "View details" }), _jsx("pre", { children: result.details })] })) : ('—') })] }, result.id));
                                            }) })] }) }), _jsxs("div", { className: "pagination", children: [_jsx("button", { type: "button", onClick: () => setOffset(Math.max(0, offset - limit)), disabled: offset === 0 || resultsLoading, className: "btn-secondary", children: "Previous" }), _jsxs("span", { className: "pagination-info", children: ["Page ", Math.floor(offset / limit) + 1, " of ", Math.ceil(pagination.total / limit) || 1] }), _jsx("button", { type: "button", onClick: () => setOffset(offset + limit), disabled: offset + limit >= pagination.total || resultsLoading, className: "btn-secondary", children: "Next" })] })] }))] })] }));
}
