import { useSecurityDashboard } from '../hooks/useSecurityDashboard';
import './SecurityDashboard.css';

function formatRelativeTime(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
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
      return 'sec-dash-status-open';
    case 'investigating':
      return 'sec-dash-status-investigating';
    case 'resolved':
      return 'sec-dash-status-resolved';
    case 'closed':
      return 'sec-dash-status-closed';
    default:
      return '';
  }
}

export function SecurityDashboard(): JSX.Element {
  const {
    activeAlerts,
    openIncidents,
    complianceScore,
    monitoredNodes,
    recentAlerts,
    recentIncidents,
    topRules,
    loading,
    error,
  } = useSecurityDashboard();

  if (loading) {
    return (
      <div className="sec-dash-page">
        <div className="sec-dash-loading">Loading security overview...</div>
      </div>
    );
  }

  return (
    <div className="sec-dash-page">
      <div className="page-header">
        <div>
          <h1>Security Overview</h1>
          <p className="subtitle">Real-time security posture and threat monitoring</p>
        </div>
      </div>

      {error && (
        <div className="error-banner">
          <p>Error loading security data: {error}</p>
        </div>
      )}

      <div className="sec-dash-stats-grid">
        <div className="sec-dash-stat-card sec-dash-stat-alerts">
          <div className="sec-dash-stat-value">{activeAlerts}</div>
          <div className="sec-dash-stat-label">Active Alerts</div>
        </div>
        <div className="sec-dash-stat-card sec-dash-stat-incidents">
          <div className="sec-dash-stat-value">{openIncidents}</div>
          <div className="sec-dash-stat-label">Open Incidents</div>
        </div>
        <div className="sec-dash-stat-card sec-dash-stat-compliance">
          <div className="sec-dash-stat-value">{complianceScore}%</div>
          <div className="sec-dash-stat-label">Compliance Score</div>
        </div>
        <div className="sec-dash-stat-card sec-dash-stat-nodes">
          <div className="sec-dash-stat-value">{monitoredNodes}</div>
          <div className="sec-dash-stat-label">Monitored Nodes</div>
        </div>
      </div>

      <div className="sec-dash-two-col">
        <div className="sec-dash-panel">
          <h2>Recent Alerts</h2>
          <ul className="sec-dash-alert-list">
            {recentAlerts.map((alert) => (
              <li key={alert.id} className="sec-dash-alert-item">
                <div className="sec-dash-alert-top">
                  <span className={`sec-dash-severity-chip ${getSeverityClass(alert.severity)}`}>
                    {alert.severity.toUpperCase()}
                  </span>
                  <span className="sec-dash-time">{formatRelativeTime(alert.createdAt)}</span>
                </div>
                <div className="sec-dash-alert-message">{alert.message}</div>
                <div className="sec-dash-alert-source">{alert.source}</div>
              </li>
            ))}
          </ul>
        </div>

        <div className="sec-dash-panel">
          <h2>Recent Incidents</h2>
          <ul className="sec-dash-incident-list">
            {recentIncidents.map((incident) => (
              <li key={incident.id} className="sec-dash-incident-item">
                <div className="sec-dash-incident-top">
                  <span className={`sec-dash-status-chip ${getStatusClass(incident.status)}`}>
                    {incident.status}
                  </span>
                  <span className="sec-dash-time">{formatRelativeTime(incident.createdAt)}</span>
                </div>
                <div className="sec-dash-incident-title">{incident.title}</div>
                <div className="sec-dash-incident-meta">
                  <span className={`sec-dash-severity-chip-sm ${getSeverityClass(incident.severity)}`}>
                    {incident.severity}
                  </span>
                  <span>{incident.relatedAlerts} related alerts</span>
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>

      <div className="sec-dash-rules-section">
        <h2>Top Triggered Rules</h2>
        <div className="sec-dash-rules-table-container">
          <table className="sec-dash-rules-table">
            <thead>
              <tr>
                <th>Rule Name</th>
                <th>Trigger Count</th>
                <th>Last Triggered</th>
              </tr>
            </thead>
            <tbody>
              {topRules.map((rule) => (
                <tr key={rule.name}>
                  <td className="sec-dash-rule-name">{rule.name}</td>
                  <td className="sec-dash-rule-count">{rule.triggerCount}</td>
                  <td className="sec-dash-rule-time">{formatRelativeTime(rule.lastTriggered)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
