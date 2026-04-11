import { useMemo, useState } from 'react';
import { useComplianceResults, useComplianceSummary, useComplianceTrends } from '../hooks/useCompliance';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { ComplianceResult } from '../lib/api';
import './Compliance.css';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function getSeverityColor(severity?: string): string {
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

function exportToCSV(results: ComplianceResult[]): void {
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

export function Compliance(): JSX.Element {
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [passedFilter, setPassedFilter] = useState<boolean | undefined>(undefined);
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });

  const {
    data: summary,
    loading: summaryLoading,
    error: summaryError,
    reload: reloadSummary,
  } = useComplianceSummary({
    tenant_id: selectedTenant,
    node_id: selectedNode,
  });

  const {
    data: trends,
    loading: trendsLoading,
    error: _trendsError,
  } = useComplianceTrends({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    days: 30,
  });

  const {
    data: results,
    loading: resultsLoading,
    error: resultsError,
    pagination,
    reload: reloadResults,
  } = useComplianceResults({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    severity: severityFilter || undefined,
    passed: passedFilter,
    limit,
    offset,
  });

  const complianceScore = useMemo(() => {
    if (!summary || summary.total === 0) return null;
    return Math.round((summary.passed / summary.total) * 100);
  }, [summary]);

  const severityBreakdown = useMemo(() => {
    if (!summary) return [];
    return Object.entries(summary.by_severity || {})
      .map(([severity, count]) => ({ severity, count }))
      .sort((a, b) => {
        const order: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3, info: 4 };
        return (order[a.severity.toLowerCase()] ?? 99) - (order[b.severity.toLowerCase()] ?? 99);
      });
  }, [summary]);

  const handleExport = () => {
    if (results.length === 0) return;
    exportToCSV(results);
  };

  const handleRefresh = () => {
    reloadSummary();
    reloadResults();
  };

  return (
    <div className="compliance-page">
      <div className="page-header">
        <div>
          <h1>Compliance Dashboard</h1>
          <p className="subtitle">Monitor compliance status, violations, and trends across your infrastructure</p>
        </div>
        <div className="page-actions">
          <button type="button" onClick={handleRefresh} className="btn-secondary">
            Refresh
          </button>
          <button type="button" onClick={handleExport} className="btn-primary" disabled={results.length === 0}>
            Export CSV
          </button>
        </div>
      </div>

      <div className="filters-section">
        <div className="filter-group">
          <label htmlFor="tenant-filter">Tenant</label>
          <select
            id="tenant-filter"
            value={selectedTenant || ''}
            onChange={(e) => {
              setSelectedTenant(e.target.value || undefined);
              setSelectedNode(undefined);
              setOffset(0);
            }}
          >
            <option value="">All Tenants</option>
            {tenants.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="node-filter">Node</label>
          <select
            id="node-filter"
            value={selectedNode || ''}
            onChange={(e) => {
              setSelectedNode(e.target.value || undefined);
              setOffset(0);
            }}
            disabled={!selectedTenant}
          >
            <option value="">All Nodes</option>
            {nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.hostname}
              </option>
            ))}
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="severity-filter">Severity</label>
          <select
            id="severity-filter"
            value={severityFilter}
            onChange={(e) => {
              setSeverityFilter(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">All Severities</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="passed-filter">Status</label>
          <select
            id="passed-filter"
            value={passedFilter === undefined ? '' : passedFilter.toString()}
            onChange={(e) => {
              const value = e.target.value;
              setPassedFilter(value === '' ? undefined : value === 'true');
              setOffset(0);
            }}
          >
            <option value="">All</option>
            <option value="true">Passed</option>
            <option value="false">Failed</option>
          </select>
        </div>
      </div>

      {summaryError && (
        <div className="error-banner">
          <p>Error loading compliance summary: {summaryError}</p>
        </div>
      )}

      {resultsError && (
        <div className="error-banner">
          <p>Error loading compliance results: {resultsError}</p>
        </div>
      )}

      <div className="compliance-overview">
        <div className="score-card">
          <div className="score-value">
            {summaryLoading ? (
              <span className="loading">—</span>
            ) : complianceScore !== null ? (
              <>
                <span className="score-number">{complianceScore}</span>
                <span className="score-unit">%</span>
              </>
            ) : (
              '—'
            )}
          </div>
          <div className="score-label">Compliance Score</div>
          {summary && (
            <div className="score-details">
              {summary.passed} of {summary.total} checks passed
            </div>
          )}
        </div>

        <div className="stats-grid">
          <div className="stat-card">
            <div className="stat-value">{summaryLoading ? '—' : summary?.total || 0}</div>
            <div className="stat-label">Total Checks</div>
          </div>
          <div className="stat-card success">
            <div className="stat-value">{summaryLoading ? '—' : summary?.passed || 0}</div>
            <div className="stat-label">Passed</div>
          </div>
          <div className="stat-card error">
            <div className="stat-value">{summaryLoading ? '—' : summary?.failed || 0}</div>
            <div className="stat-label">Failed</div>
          </div>
        </div>
      </div>

      {severityBreakdown.length > 0 && (
        <div className="severity-breakdown">
          <h2>Violations by Severity</h2>
          <div className="severity-list">
            {severityBreakdown.map(({ severity, count }) => (
              <div key={severity} className="severity-item">
                <div className="severity-indicator" style={{ backgroundColor: getSeverityColor(severity) }} />
                <span className="severity-name">{severity.toUpperCase()}</span>
                <span className="severity-count">{count}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {trends.length > 0 && (
        <div className="trends-section">
          <h2>Compliance Trends (Last 30 Days)</h2>
          <div className="trends-chart">
            {trendsLoading ? (
              <div className="loading-placeholder">Loading trends...</div>
            ) : (
              <div className="trends-bars">
                {trends.map((trend, idx) => {
                  const maxValue = Math.max(...trends.map((t) => t.total));
                  const passedPercent = maxValue > 0 ? (trend.passed / maxValue) * 100 : 0;
                  const failedPercent = maxValue > 0 ? (trend.failed / maxValue) * 100 : 0;
                  const date = new Date(trend.date);
                  return (
                    <div key={idx} className="trend-bar-group">
                      <div className="trend-bar-container">
                        <div className="trend-bar passed" style={{ height: `${passedPercent}%` }} title={`Passed: ${trend.passed}`} />
                        <div className="trend-bar failed" style={{ height: `${failedPercent}%` }} title={`Failed: ${trend.failed}`} />
                      </div>
                      <div className="trend-label">{date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })}</div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}

      <div className="results-section">
        <div className="section-header">
          <h2>Compliance Results</h2>
          <div className="results-count">
            Showing {results.length} of {pagination.total}
          </div>
        </div>

        {resultsLoading ? (
          <div className="loading-placeholder">Loading compliance results...</div>
        ) : results.length === 0 ? (
          <div className="empty-state">
            <p>No compliance results found matching your filters.</p>
          </div>
        ) : (
          <>
            <div className="results-table-container">
              <table className="results-table">
                <thead>
                  <tr>
                    <th>Rule ID</th>
                    <th>Node</th>
                    <th>Status</th>
                    <th>Severity</th>
                    <th>Checked At</th>
                    <th>Details</th>
                  </tr>
                </thead>
                <tbody>
                  {results.map((result) => {
                    const node = nodes.find((n) => n.id === result.node_id);
                    return (
                      <tr key={result.id} className={result.passed ? 'passed' : 'failed'}>
                        <td>
                          <code>{result.rule_id}</code>
                        </td>
                        <td>{node?.hostname || result.node_id || '—'}</td>
                        <td>
                          <span className={`status-badge ${result.passed ? 'success' : 'error'}`}>
                            {result.passed ? 'Passed' : 'Failed'}
                          </span>
                        </td>
                        <td>
                          {result.severity && (
                            <span
                              className="severity-badge"
                              style={{
                                backgroundColor: getSeverityColor(result.severity),
                                color: '#fff',
                                padding: '2px 8px',
                                borderRadius: '4px',
                                fontSize: '0.875rem',
                              }}
                            >
                              {result.severity.toUpperCase()}
                            </span>
                          )}
                        </td>
                        <td>{formatDate(result.checked_at)}</td>
                        <td className="details-cell">
                          {result.details ? (
                            <details>
                              <summary>View details</summary>
                              <pre>{result.details}</pre>
                            </details>
                          ) : (
                            '—'
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            <div className="pagination">
              <button
                type="button"
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0 || resultsLoading}
                className="btn-secondary"
              >
                Previous
              </button>
              <span className="pagination-info">
                Page {Math.floor(offset / limit) + 1} of {Math.ceil(pagination.total / limit) || 1}
              </span>
              <button
                type="button"
                onClick={() => setOffset(offset + limit)}
                disabled={offset + limit >= pagination.total || resultsLoading}
                className="btn-secondary"
              >
                Next
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

