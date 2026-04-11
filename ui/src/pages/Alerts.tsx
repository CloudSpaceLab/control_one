import { useState } from 'react';
import { useAlerts } from '../hooks/useAlerts';
import './Alerts.css';

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

function getStatusClass(status: string): string {
  switch (status) {
    case 'open':
      return 'alert-status-open';
    case 'acknowledged':
      return 'alert-status-acknowledged';
    case 'resolved':
      return 'alert-status-resolved';
    default:
      return '';
  }
}

export function Alerts(): JSX.Element {
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<string>('');

  const { data: alerts, loading, error } = useAlerts({
    severity: severityFilter || undefined,
    status: statusFilter || undefined,
  });

  const stats = {
    total24h: alerts.length,
    critical: alerts.filter((a) => a.severity === 'critical').length,
    acknowledged: alerts.filter((a) => a.status === 'acknowledged').length,
    unresolved: alerts.filter((a) => a.status !== 'resolved').length,
  };

  return (
    <div className="alerts-page">
      <div className="page-header">
        <div>
          <h1>Alerts</h1>
          <p className="subtitle">Security alerts and rule management</p>
        </div>
      </div>

      {error && (
        <div className="error-banner">
          <p>Error loading alerts: {error}</p>
        </div>
      )}

      <div className="alerts-stats-grid">
        <div className="alerts-stat-card">
          <div className="alerts-stat-value">{stats.total24h}</div>
          <div className="alerts-stat-label">Total (24h)</div>
        </div>
        <div className="alerts-stat-card alerts-stat-critical">
          <div className="alerts-stat-value">{stats.critical}</div>
          <div className="alerts-stat-label">Critical</div>
        </div>
        <div className="alerts-stat-card alerts-stat-acknowledged">
          <div className="alerts-stat-value">{stats.acknowledged}</div>
          <div className="alerts-stat-label">Acknowledged</div>
        </div>
        <div className="alerts-stat-card alerts-stat-unresolved">
          <div className="alerts-stat-value">{stats.unresolved}</div>
          <div className="alerts-stat-label">Unresolved</div>
        </div>
      </div>

      <div className="alerts-filters">
        <div className="alerts-filter-group">
          <label htmlFor="severity-filter">Severity</label>
          <select
            id="severity-filter"
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
        <div className="alerts-filter-group">
          <label htmlFor="status-filter">Status</label>
          <select
            id="status-filter"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
          >
            <option value="">All Statuses</option>
            <option value="open">Open</option>
            <option value="acknowledged">Acknowledged</option>
            <option value="resolved">Resolved</option>
          </select>
        </div>
      </div>

      {loading ? (
        <div className="alerts-loading">Loading alerts...</div>
      ) : alerts.length === 0 ? (
        <div className="alerts-empty">
          <p>No alerts found matching your filters.</p>
        </div>
      ) : (
        <div className="alerts-table-section">
          <div className="alerts-table-container">
            <table className="alerts-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Severity</th>
                  <th>Rule</th>
                  <th>Source</th>
                  <th>Message</th>
                  <th>Status</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {alerts.map((alert) => (
                  <tr key={alert.id}>
                    <td className="alerts-time-cell">{formatDate(alert.createdAt)}</td>
                    <td>
                      <span className={`alerts-severity-chip ${getSeverityClass(alert.severity)}`}>
                        {alert.severity.toUpperCase()}
                      </span>
                    </td>
                    <td className="alerts-rule-cell">{alert.ruleName}</td>
                    <td><code>{alert.source}</code></td>
                    <td className="alerts-message-cell">{alert.message}</td>
                    <td>
                      <span className={`alerts-status-chip ${getStatusClass(alert.status)}`}>
                        {alert.status}
                      </span>
                    </td>
                    <td className="alerts-actions-cell">
                      {alert.status === 'open' && (
                        <button type="button" className="alerts-btn-action">Acknowledge</button>
                      )}
                      {alert.status !== 'resolved' && (
                        <button type="button" className="alerts-btn-action alerts-btn-resolve">Resolve</button>
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
