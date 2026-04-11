import { useState } from 'react';
import { useIncidents } from '../hooks/useIncidents';
import './Incidents.css';

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function getSeverityClass(severity: string): string {
  switch (severity) {
    case 'critical':
      return 'severity-critical';
    case 'high':
      return 'severity-high';
    case 'medium':
      return 'severity-medium';
    case 'low':
      return 'severity-low';
    default:
      return '';
  }
}

function getIncidentStatusClass(status: string): string {
  switch (status) {
    case 'open':
      return 'incident-status-open';
    case 'investigating':
      return 'incident-status-investigating';
    case 'resolved':
      return 'incident-status-resolved';
    case 'closed':
      return 'incident-status-closed';
    default:
      return '';
  }
}

export function Incidents(): JSX.Element {
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [severityFilter, setSeverityFilter] = useState<string>('');

  const { data: incidents, loading, error } = useIncidents({
    status: statusFilter || undefined,
    severity: severityFilter || undefined,
  });

  const stats = {
    open: incidents.filter((i) => i.status === 'open').length,
    investigating: incidents.filter((i) => i.status === 'investigating').length,
    resolved30d: incidents.filter((i) => {
      if (!i.resolvedAt) return false;
      const resolved = new Date(i.resolvedAt);
      const thirtyDaysAgo = new Date(Date.now() - 30 * 24 * 3600000);
      return resolved >= thirtyDaysAgo;
    }).length,
    avgResolution: '4.2h',
  };

  return (
    <div className="incidents-page">
      <div className="page-header">
        <div>
          <h1>Incidents</h1>
          <p className="subtitle">Security incident tracking and management</p>
        </div>
        <div className="page-actions">
          <button type="button" className="incidents-btn-create">Create Incident</button>
        </div>
      </div>

      {error && (
        <div className="error-banner">
          <p>Error loading incidents: {error}</p>
        </div>
      )}

      <div className="incidents-stats-grid">
        <div className="incidents-stat-card incidents-stat-open">
          <div className="incidents-stat-value">{stats.open}</div>
          <div className="incidents-stat-label">Open</div>
        </div>
        <div className="incidents-stat-card incidents-stat-investigating">
          <div className="incidents-stat-value">{stats.investigating}</div>
          <div className="incidents-stat-label">Investigating</div>
        </div>
        <div className="incidents-stat-card incidents-stat-resolved">
          <div className="incidents-stat-value">{stats.resolved30d}</div>
          <div className="incidents-stat-label">Resolved (30d)</div>
        </div>
        <div className="incidents-stat-card">
          <div className="incidents-stat-value">{stats.avgResolution}</div>
          <div className="incidents-stat-label">Avg Resolution Time</div>
        </div>
      </div>

      <div className="incidents-filters">
        <div className="incidents-filter-group">
          <label htmlFor="incidents-status-filter">Status</label>
          <select
            id="incidents-status-filter"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
          >
            <option value="">All Statuses</option>
            <option value="open">Open</option>
            <option value="investigating">Investigating</option>
            <option value="resolved">Resolved</option>
            <option value="closed">Closed</option>
          </select>
        </div>
        <div className="incidents-filter-group">
          <label htmlFor="incidents-severity-filter">Severity</label>
          <select
            id="incidents-severity-filter"
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
          >
            <option value="">All Severities</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
      </div>

      {loading ? (
        <div className="incidents-loading">Loading incidents...</div>
      ) : incidents.length === 0 ? (
        <div className="incidents-empty">
          <p>No incidents found matching your filters.</p>
        </div>
      ) : (
        <div className="incidents-table-section">
          <div className="incidents-table-container">
            <table className="incidents-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Title</th>
                  <th>Severity</th>
                  <th>Status</th>
                  <th>Assigned To</th>
                  <th>Related Alerts</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {incidents.map((incident) => (
                  <tr key={incident.id}>
                    <td><code>{incident.id}</code></td>
                    <td className="incidents-title-cell">{incident.title}</td>
                    <td>
                      <span className={`incidents-severity-chip ${getSeverityClass(incident.severity)}`}>
                        {incident.severity.toUpperCase()}
                      </span>
                    </td>
                    <td>
                      <span className={`incidents-status-chip ${getIncidentStatusClass(incident.status)}`}>
                        {incident.status}
                      </span>
                    </td>
                    <td>{incident.assignedTo || '—'}</td>
                    <td className="incidents-alerts-count">{incident.relatedAlerts}</td>
                    <td className="incidents-time-cell">{formatDate(incident.createdAt)}</td>
                    <td className="incidents-actions-cell">
                      <button type="button" className="incidents-btn-action">View</button>
                      {incident.status !== 'closed' && incident.status !== 'resolved' && (
                        <button type="button" className="incidents-btn-action incidents-btn-escalate">Escalate</button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
