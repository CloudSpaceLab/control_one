import { useMemo, useState } from 'react';
import { useTelemetryMetrics, useTelemetryLogs } from '../hooks/useTelemetry';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { TelemetryMetric, TelemetryLog } from '../lib/api';
import './Telemetry.css';

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

function getLogLevelColor(level: string): string {
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

export function Telemetry(): JSX.Element {
  const [viewMode, setViewMode] = useState<'metrics' | 'logs'>('metrics');
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedNode, setSelectedNode] = useState<string | undefined>(undefined);
  const [metricNameFilter, setMetricNameFilter] = useState<string>('');
  const [logLevelFilter, setLogLevelFilter] = useState<string>('');
  const [logSourceFilter, setLogSourceFilter] = useState<string>('');
  const [limit] = useState(100);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes({ tenantId: selectedTenant, limit: 1000 });

  const {
    data: metrics,
    loading: metricsLoading,
    error: metricsError,
    pagination: metricsPagination,
    reload: reloadMetrics,
  } = useTelemetryMetrics({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    metric_name: metricNameFilter || undefined,
    limit,
    offset: viewMode === 'metrics' ? offset : 0,
  });

  const {
    data: logs,
    loading: logsLoading,
    error: logsError,
    pagination: logsPagination,
    reload: reloadLogs,
  } = useTelemetryLogs({
    tenant_id: selectedTenant,
    node_id: selectedNode,
    log_level: logLevelFilter || undefined,
    log_source: logSourceFilter || undefined,
    limit,
    offset: viewMode === 'logs' ? offset : 0,
  });

  const uniqueMetricNames = useMemo(() => {
    const names = new Set<string>();
    metrics.forEach((m) => names.add(m.metric_name));
    return Array.from(names).sort();
  }, [metrics]);

  const uniqueLogLevels = useMemo(() => {
    const levels = new Set<string>();
    logs.forEach((l) => levels.add(l.log_level));
    return Array.from(levels).sort();
  }, [logs]);

  const uniqueLogSources = useMemo(() => {
    const sources = new Set<string>();
    logs.forEach((l) => {
      if (l.log_source) sources.add(l.log_source);
    });
    return Array.from(sources).sort();
  }, [logs]);

  const handleRefresh = () => {
    if (viewMode === 'metrics') {
      reloadMetrics();
    } else {
      reloadLogs();
    }
  };

  const currentPagination = viewMode === 'metrics' ? metricsPagination : logsPagination;
  const currentLoading = viewMode === 'metrics' ? metricsLoading : logsLoading;
  const currentError = viewMode === 'metrics' ? metricsError : logsError;

  return (
    <div className="telemetry-page">
      <div className="page-header">
        <div>
          <h1>Telemetry Dashboard</h1>
          <p className="subtitle">Monitor metrics and logs from your infrastructure</p>
        </div>
        <div className="page-actions">
          <div className="view-mode-toggle">
            <button
              type="button"
              className={viewMode === 'metrics' ? 'btn-primary' : 'btn-secondary'}
              onClick={() => {
                setViewMode('metrics');
                setOffset(0);
              }}
            >
              Metrics
            </button>
            <button
              type="button"
              className={viewMode === 'logs' ? 'btn-primary' : 'btn-secondary'}
              onClick={() => {
                setViewMode('logs');
                setOffset(0);
              }}
            >
              Logs
            </button>
          </div>
          <button type="button" onClick={handleRefresh} className="btn-secondary">
            Refresh
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

        {viewMode === 'metrics' && (
          <div className="filter-group">
            <label htmlFor="metric-name-filter">Metric Name</label>
            <select
              id="metric-name-filter"
              value={metricNameFilter}
              onChange={(e) => {
                setMetricNameFilter(e.target.value);
                setOffset(0);
              }}
            >
              <option value="">All Metrics</option>
              {uniqueMetricNames.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>
        )}

        {viewMode === 'logs' && (
          <>
            <div className="filter-group">
              <label htmlFor="log-level-filter">Log Level</label>
              <select
                id="log-level-filter"
                value={logLevelFilter}
                onChange={(e) => {
                  setLogLevelFilter(e.target.value);
                  setOffset(0);
                }}
              >
                <option value="">All Levels</option>
                {uniqueLogLevels.map((level) => (
                  <option key={level} value={level}>
                    {level.toUpperCase()}
                  </option>
                ))}
              </select>
            </div>

            <div className="filter-group">
              <label htmlFor="log-source-filter">Log Source</label>
              <select
                id="log-source-filter"
                value={logSourceFilter}
                onChange={(e) => {
                  setLogSourceFilter(e.target.value);
                  setOffset(0);
                }}
              >
                <option value="">All Sources</option>
                {uniqueLogSources.map((source) => (
                  <option key={source} value={source}>
                    {source}
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
      </div>

      {currentError && (
        <div className="error-banner">
          <p>Error loading telemetry data: {currentError}</p>
        </div>
      )}

      {viewMode === 'metrics' ? (
        <div className="telemetry-content">
          <div className="section-header">
            <h2>Metrics</h2>
            <div className="results-count">
              Showing {metrics.length} of {currentPagination.total}
            </div>
          </div>

          {currentLoading ? (
            <div className="loading-placeholder">Loading metrics...</div>
          ) : metrics.length === 0 ? (
            <div className="empty-state">
              <p>No metrics found matching your filters.</p>
            </div>
          ) : (
            <>
              <div className="table-container">
                <table className="telemetry-table">
                  <thead>
                    <tr>
                      <th>Timestamp</th>
                      <th>Metric Name</th>
                      <th>Value</th>
                      <th>Unit</th>
                      <th>Node</th>
                      <th>Labels</th>
                    </tr>
                  </thead>
                  <tbody>
                    {metrics.map((metric) => {
                      const node = nodes.find((n) => n.id === metric.node_id);
                      return (
                        <tr key={metric.id}>
                          <td>{formatDate(metric.timestamp)}</td>
                          <td>
                            <code>{metric.metric_name}</code>
                          </td>
                          <td className="metric-value">{metric.metric_value.toLocaleString()}</td>
                          <td>{metric.metric_unit || '—'}</td>
                          <td>{node?.hostname || metric.node_id || '—'}</td>
                          <td>
                            {metric.labels && Object.keys(metric.labels).length > 0 ? (
                              <details>
                                <summary>View labels</summary>
                                <pre>{JSON.stringify(metric.labels, null, 2)}</pre>
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
                  disabled={offset === 0 || currentLoading}
                  className="btn-secondary"
                >
                  Previous
                </button>
                <span className="pagination-info">
                  Page {Math.floor(offset / limit) + 1} of {Math.ceil(currentPagination.total / limit) || 1}
                </span>
                <button
                  type="button"
                  onClick={() => setOffset(offset + limit)}
                  disabled={offset + limit >= currentPagination.total || currentLoading}
                  className="btn-secondary"
                >
                  Next
                </button>
              </div>
            </>
          )}
        </div>
      ) : (
        <div className="telemetry-content">
          <div className="section-header">
            <h2>Logs</h2>
            <div className="results-count">
              Showing {logs.length} of {currentPagination.total}
            </div>
          </div>

          {currentLoading ? (
            <div className="loading-placeholder">Loading logs...</div>
          ) : logs.length === 0 ? (
            <div className="empty-state">
              <p>No logs found matching your filters.</p>
            </div>
          ) : (
            <>
              <div className="logs-container">
                {logs.map((log) => {
                  const node = nodes.find((n) => n.id === log.node_id);
                  return (
                    <div key={log.id} className="log-entry">
                      <div className="log-header">
                        <span
                          className="log-level-badge"
                          style={{ backgroundColor: getLogLevelColor(log.log_level) }}
                        >
                          {log.log_level.toUpperCase()}
                        </span>
                        <span className="log-timestamp">{formatDate(log.timestamp)}</span>
                        {log.log_source && <span className="log-source">{log.log_source}</span>}
                        {log.log_program && <span className="log-program">{log.log_program}</span>}
                        {node && <span className="log-node">{node.hostname}</span>}
                      </div>
                      <div className="log-message">{log.log_message}</div>
                      {log.labels && Object.keys(log.labels).length > 0 && (
                        <details className="log-labels">
                          <summary>Labels</summary>
                          <pre>{JSON.stringify(log.labels, null, 2)}</pre>
                        </details>
                      )}
                    </div>
                  );
                })}
              </div>

              <div className="pagination">
                <button
                  type="button"
                  onClick={() => setOffset(Math.max(0, offset - limit))}
                  disabled={offset === 0 || currentLoading}
                  className="btn-secondary"
                >
                  Previous
                </button>
                <span className="pagination-info">
                  Page {Math.floor(offset / limit) + 1} of {Math.ceil(currentPagination.total / limit) || 1}
                </span>
                <button
                  type="button"
                  onClick={() => setOffset(offset + limit)}
                  disabled={offset + limit >= currentPagination.total || currentLoading}
                  className="btn-secondary"
                >
                  Next
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

