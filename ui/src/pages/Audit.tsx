import { useMemo, useState } from 'react';
import { useAuditLogs } from '../hooks/useAuditLogs';
import { useTenants } from '../hooks/useTenants';
import { AuditLog } from '../lib/api';
import './Audit.css';

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

function formatRelativeTime(value?: string): string {
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

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return parsed.toLocaleDateString();
}

function getActionColor(action: string): string {
  if (action.includes('.create') || action.includes('.created')) return '#10b981';
  if (action.includes('.update') || action.includes('.updated')) return '#3b82f6';
  if (action.includes('.delete') || action.includes('.deleted')) return '#ef4444';
  if (action.includes('.failed') || action.includes('.error')) return '#dc2626';
  if (action.includes('.success') || action.includes('.succeeded')) return '#10b981';
  return '#6b7280';
}

function exportToCSV(logs: AuditLog[]): void {
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

export function Audit(): JSX.Element {
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [actorTypeFilter, setActorTypeFilter] = useState<string>('');
  const [actionFilter, setActionFilter] = useState<string>('');
  const [resourceTypeFilter, setResourceTypeFilter] = useState<string>('');
  const [viewMode, setViewMode] = useState<'table' | 'timeline'>('table');
  const [limit] = useState(100);
  const [offset, setOffset] = useState(0);

  const { data: tenants } = useTenants();

  const {
    data: logs,
    loading,
    error,
    pagination,
    reload,
  } = useAuditLogs({
    tenant_id: selectedTenant,
    actor_type: actorTypeFilter || undefined,
    action: actionFilter || undefined,
    resource_type: resourceTypeFilter || undefined,
    limit,
    offset,
  });

  const uniqueActions = useMemo(() => {
    const actions = new Set<string>();
    logs.forEach((log) => actions.add(log.action));
    return Array.from(actions).sort();
  }, [logs]);

  const uniqueResourceTypes = useMemo(() => {
    const types = new Set<string>();
    logs.forEach((log) => types.add(log.resource_type));
    return Array.from(types).sort();
  }, [logs]);

  const handleExport = () => {
    if (logs.length === 0) return;
    exportToCSV(logs);
  };

  const handleRefresh = () => {
    reload();
  };

  return (
    <div className="audit-page">
      <div className="page-header">
        <div>
          <h1>Audit Log</h1>
          <p className="subtitle">View and search audit trail of all system activities</p>
        </div>
        <div className="page-actions">
          <button type="button" onClick={handleRefresh} className="btn-secondary">
            Refresh
          </button>
          <button type="button" onClick={handleExport} className="btn-primary" disabled={logs.length === 0}>
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
          <label htmlFor="actor-type-filter">Actor Type</label>
          <select
            id="actor-type-filter"
            value={actorTypeFilter}
            onChange={(e) => {
              setActorTypeFilter(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">All Types</option>
            <option value="user">User</option>
            <option value="system">System</option>
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="action-filter">Action</label>
          <select
            id="action-filter"
            value={actionFilter}
            onChange={(e) => {
              setActionFilter(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">All Actions</option>
            {uniqueActions.map((action) => (
              <option key={action} value={action}>
                {action}
              </option>
            ))}
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="resource-type-filter">Resource Type</label>
          <select
            id="resource-type-filter"
            value={resourceTypeFilter}
            onChange={(e) => {
              setResourceTypeFilter(e.target.value);
              setOffset(0);
            }}
          >
            <option value="">All Resources</option>
            {uniqueResourceTypes.map((type) => (
              <option key={type} value={type}>
                {type}
              </option>
            ))}
          </select>
        </div>

        <div className="filter-group">
          <label htmlFor="view-mode-filter">View</label>
          <select
            id="view-mode-filter"
            value={viewMode}
            onChange={(e) => setViewMode(e.target.value as 'table' | 'timeline')}
          >
            <option value="table">Table</option>
            <option value="timeline">Timeline</option>
          </select>
        </div>
      </div>

      {error && (
        <div className="error-banner">
          <p>Error loading audit logs: {error}</p>
        </div>
      )}

      <div className="audit-stats">
        <div className="stat-card">
          <div className="stat-value">{pagination.total}</div>
          <div className="stat-label">Total Events</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">{logs.filter((l) => l.actor_type === 'user').length}</div>
          <div className="stat-label">User Actions</div>
        </div>
        <div className="stat-card">
          <div className="stat-value">{logs.filter((l) => l.actor_type === 'system').length}</div>
          <div className="stat-label">System Events</div>
        </div>
      </div>

      {loading ? (
        <div className="loading-placeholder">Loading audit logs...</div>
      ) : logs.length === 0 ? (
        <div className="empty-state">
          <p>No audit logs found matching your filters.</p>
        </div>
      ) : viewMode === 'table' ? (
        <>
          <div className="results-section">
            <div className="section-header">
              <h2>Audit Log Entries</h2>
              <div className="results-count">
                Showing {logs.length} of {pagination.total}
              </div>
            </div>

            <div className="table-container">
              <table className="audit-table">
                <thead>
                  <tr>
                    <th>Timestamp</th>
                    <th>Actor</th>
                    <th>Action</th>
                    <th>Resource</th>
                    <th>Details</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.map((log) => (
                    <tr key={log.id}>
                      <td className="timestamp-cell">
                        <div className="timestamp-primary">{formatDate(log.created_at)}</div>
                        <div className="timestamp-secondary">{formatRelativeTime(log.created_at)}</div>
                      </td>
                      <td>
                        <span className="actor-badge">{log.actor_type}</span>
                        {log.actor_id && <span className="actor-id">({log.actor_id.slice(0, 8)}...)</span>}
                      </td>
                      <td>
                        <span className="action-badge" style={{ backgroundColor: getActionColor(log.action) }}>
                          {log.action}
                        </span>
                      </td>
                      <td>
                        <div className="resource-info">
                          <span className="resource-type">{log.resource_type}</span>
                          {log.resource_id && (
                            <code className="resource-id" title={log.resource_id}>
                              {log.resource_id.length > 20 ? `${log.resource_id.slice(0, 20)}...` : log.resource_id}
                            </code>
                          )}
                        </div>
                      </td>
                      <td className="details-cell">
                        {log.metadata && Object.keys(log.metadata).length > 0 ? (
                          <details>
                            <summary>View metadata</summary>
                            <pre>{JSON.stringify(log.metadata, null, 2)}</pre>
                          </details>
                        ) : (
                          '—'
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="pagination">
              <button
                type="button"
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0 || loading}
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
                disabled={offset + limit >= pagination.total || loading}
                className="btn-secondary"
              >
                Next
              </button>
            </div>
          </div>
        </>
      ) : (
        <div className="timeline-section">
          <h2>Timeline View</h2>
          <div className="timeline">
            {logs.map((log, idx) => (
              <div key={log.id} className="timeline-item">
                <div className="timeline-marker" style={{ backgroundColor: getActionColor(log.action) }} />
                <div className="timeline-content">
                  <div className="timeline-header">
                    <span className="timeline-action" style={{ color: getActionColor(log.action) }}>
                      {log.action}
                    </span>
                    <span className="timeline-time">{formatRelativeTime(log.created_at)}</span>
                  </div>
                  <div className="timeline-body">
                    <div className="timeline-details">
                      <span className="detail-label">Actor:</span>
                      <span className="detail-value">{log.actor_type}</span>
                      {log.actor_id && <span className="detail-value secondary">({log.actor_id.slice(0, 8)}...)</span>}
                    </div>
                    <div className="timeline-details">
                      <span className="detail-label">Resource:</span>
                      <span className="detail-value">{log.resource_type}</span>
                      {log.resource_id && (
                        <code className="detail-value secondary">{log.resource_id.slice(0, 20)}...</code>
                      )}
                    </div>
                    {log.metadata && Object.keys(log.metadata).length > 0 && (
                      <details className="timeline-metadata">
                        <summary>Metadata</summary>
                        <pre>{JSON.stringify(log.metadata, null, 2)}</pre>
                      </details>
                    )}
                  </div>
                  <div className="timeline-footer">
                    <span className="timeline-timestamp">{formatDate(log.created_at)}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

