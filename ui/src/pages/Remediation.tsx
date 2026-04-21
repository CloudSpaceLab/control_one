import { useMemo, useState } from 'react';
import {
  useRemediationApprovals,
  useRemediationFailures,
  useRemediationStats,
  useRemediationVerificationStats,
} from '../hooks/useRemediation';
import { useTenants } from '../hooks/useTenants';
import { RemediationApproval, RemediationRuleStat } from '../lib/api';
import './Compliance.css';
import './Remediation.css';

function formatDate(value?: string): string {
  if (!value) return '—';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '—';
  return `${value.toFixed(1)}%`;
}

function severityColor(severity?: string): string {
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

function successRateClass(rate: number, total: number): string {
  if (total === 0) return 'rate-neutral';
  if (rate >= 90) return 'rate-high';
  if (rate >= 70) return 'rate-medium';
  return 'rate-low';
}

const WINDOW_OPTIONS: Array<{ value: string; label: string }> = [
  { value: '1d', label: 'Last 24h' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
];

export function Remediation(): JSX.Element {
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [windowValue, setWindowValue] = useState<string>('7d');
  const [ruleFilter, setRuleFilter] = useState<string>('');

  const { data: tenants } = useTenants();

  const statsParams = useMemo(
    () => ({ window: windowValue, tenant_id: selectedTenant }),
    [windowValue, selectedTenant],
  );
  const failuresParams = useMemo(
    () => ({ window: windowValue, tenant_id: selectedTenant, rule_id: ruleFilter || undefined }),
    [windowValue, selectedTenant, ruleFilter],
  );
  const approvalsParams = useMemo(
    () => ({ tenant_id: selectedTenant, status: 'pending', limit: 50 }),
    [selectedTenant],
  );

  const { data: stats, loading: statsLoading, error: statsError, reload: reloadStats } =
    useRemediationStats(statsParams);
  const { data: failures, loading: failuresLoading, error: failuresError, reload: reloadFailures } =
    useRemediationFailures(failuresParams);
  const {
    data: verification,
    loading: verificationLoading,
    error: verificationError,
    reload: reloadVerification,
  } = useRemediationVerificationStats(statsParams);
  const {
    data: approvals,
    loading: approvalsLoading,
    error: approvalsError,
    reload: reloadApprovals,
    approve,
    deny,
    actionState,
  } = useRemediationApprovals(approvalsParams);

  const refreshAll = () => {
    reloadStats();
    reloadFailures();
    reloadVerification();
    reloadApprovals();
  };

  const onApprove = async (a: RemediationApproval) => {
    const ok = globalThis.confirm(
      `Approve remediation for ${a.rule_id} on this node? The job will be queued immediately.`,
    );
    if (!ok) return;
    try {
      await approve(a.id);
    } catch {
      // error surfaced via actionState.error banner
    }
  };

  const onDeny = async (a: RemediationApproval) => {
    const ok = globalThis.confirm(
      `Deny the remediation request for ${a.rule_id}? The request will be archived.`,
    );
    if (!ok) return;
    try {
      await deny(a.id);
    } catch {
      // no-op
    }
  };

  const verificationRows = useMemo(() => {
    if (!verification) return [];
    return [
      { label: 'Verified', value: verification.verified, color: '#10b981' },
      { label: 'Pending verify', value: verification.pending_verify, color: '#3b82f6' },
      { label: 'Not verified', value: verification.not_verified, color: '#f59e0b' },
      { label: 'Rolled back', value: verification.rolled_back, color: '#dc2626' },
    ];
  }, [verification]);

  const maxFailures = useMemo(() => {
    if (!failures || failures.points.length === 0) return 0;
    return failures.points.reduce((acc, point) => Math.max(acc, point.total), 0);
  }, [failures]);

  return (
    <div className="compliance-page remediation-page">
      <div className="page-header">
        <div>
          <h1>Remediation Dashboard</h1>
          <p className="subtitle">
            Per-rule success rates, failure trends, pending approvals, and verification outcomes.
          </p>
        </div>
        <div className="page-actions">
          <button type="button" onClick={refreshAll} className="btn-secondary">
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
            onChange={(e) => setSelectedTenant(e.target.value || undefined)}
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
          <label htmlFor="window-filter">Window</label>
          <select
            id="window-filter"
            value={windowValue}
            onChange={(e) => setWindowValue(e.target.value)}
          >
            {WINDOW_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
        <div className="filter-group">
          <label htmlFor="rule-filter">Rule (failures chart)</label>
          <input
            id="rule-filter"
            type="text"
            value={ruleFilter}
            onChange={(e) => setRuleFilter(e.target.value)}
            placeholder="rule_id"
          />
        </div>
      </div>

      {statsError && <div className="error-banner"><p>{statsError}</p></div>}
      {verificationError && <div className="error-banner"><p>{verificationError}</p></div>}
      {failuresError && <div className="error-banner"><p>{failuresError}</p></div>}
      {approvalsError && <div className="error-banner"><p>{approvalsError}</p></div>}
      {actionState.error && (
        <div className="error-banner"><p>{actionState.error}</p></div>
      )}

      <div className="remediation-overview">
        <div className="score-card">
          <div className="score-value">
            {statsLoading ? (
              <span className="loading">—</span>
            ) : stats && stats.totals.succeeded + stats.totals.failed > 0 ? (
              <>
                <span className="score-number">{Math.round(stats.totals.success_rate)}</span>
                <span className="score-unit">%</span>
              </>
            ) : (
              '—'
            )}
          </div>
          <div className="score-label">Overall success</div>
          {stats && (
            <div className="score-details">
              {stats.totals.succeeded} of {stats.totals.succeeded + stats.totals.failed} remediations succeeded
            </div>
          )}
        </div>
        <div className="stats-grid remediation-stats-grid">
          <div className="stat-card">
            <div className="stat-value">{statsLoading ? '—' : stats?.totals.total ?? 0}</div>
            <div className="stat-label">Total runs</div>
          </div>
          <div className="stat-card success">
            <div className="stat-value">{statsLoading ? '—' : stats?.totals.succeeded ?? 0}</div>
            <div className="stat-label">Succeeded</div>
          </div>
          <div className="stat-card error">
            <div className="stat-value">{statsLoading ? '—' : stats?.totals.failed ?? 0}</div>
            <div className="stat-label">Failed</div>
          </div>
          <div className="stat-card">
            <div className="stat-value">
              {statsLoading ? '—' : (stats?.totals.running ?? 0) + (stats?.totals.queued ?? 0)}
            </div>
            <div className="stat-label">In-flight</div>
          </div>
        </div>
      </div>

      <section className="remediation-section">
        <div className="section-header">
          <h2>Per-rule success rate</h2>
          <div className="results-count">
            {statsLoading ? 'Loading…' : `${stats?.per_rule.length ?? 0} rules with activity`}
          </div>
        </div>
        {statsLoading ? (
          <div className="loading-placeholder">Loading rule stats…</div>
        ) : !stats || stats.per_rule.length === 0 ? (
          <div className="empty-state">
            <p>No remediation jobs in the selected window.</p>
          </div>
        ) : (
          <div className="results-table-container">
            <table className="results-table">
              <thead>
                <tr>
                  <th>Rule</th>
                  <th>Success rate</th>
                  <th>Succeeded</th>
                  <th>Failed</th>
                  <th>In-flight</th>
                  <th>Last run</th>
                </tr>
              </thead>
              <tbody>
                {stats.per_rule.map((row: RemediationRuleStat) => {
                  const resolved = row.succeeded + row.failed;
                  return (
                    <tr key={row.rule_id}>
                      <td><code>{row.rule_id}</code></td>
                      <td>
                        <div className={`rate-bar ${successRateClass(row.success_rate, resolved)}`}>
                          <div
                            className="rate-bar-fill"
                            style={{ width: `${Math.min(100, Math.max(0, row.success_rate))}%` }}
                          />
                          <span className="rate-bar-label">{resolved === 0 ? '—' : formatPercent(row.success_rate)}</span>
                        </div>
                      </td>
                      <td>{row.succeeded}</td>
                      <td>{row.failed}</td>
                      <td>{row.running + row.queued}</td>
                      <td>{formatDate(row.last_run_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="remediation-section">
        <div className="section-header">
          <h2>Failures timeline</h2>
          <div className="results-count">
            {failuresLoading
              ? 'Loading…'
              : failures
                ? `${failures.points.reduce((a, p) => a + p.failed, 0)} failures / ${failures.points.reduce((a, p) => a + p.total, 0)} runs`
                : ''}
          </div>
        </div>
        {failuresLoading ? (
          <div className="loading-placeholder">Loading failures…</div>
        ) : !failures || failures.points.length === 0 ? (
          <div className="empty-state"><p>No jobs landed in this window.</p></div>
        ) : (
          <div className="trends-chart">
            <div className="trends-bars">
              {failures.points.map((point) => {
                const totalHeight = maxFailures === 0 ? 0 : (point.total / maxFailures) * 100;
                const failureShare = point.total === 0 ? 0 : (point.failed / point.total) * totalHeight;
                const successShare = totalHeight - failureShare;
                const label = new Date(point.date).toLocaleDateString('en-US', {
                  month: 'short',
                  day: 'numeric',
                });
                return (
                  <div key={point.date} className="trend-bar-group">
                    <div className="trend-bar-container">
                      <div
                        className="trend-bar failed"
                        title={`${point.failed} failed / ${point.total} total`}
                        style={{ height: `${failureShare}%` }}
                      />
                      <div
                        className="trend-bar passed"
                        title={`${point.total - point.failed} succeeded`}
                        style={{ height: `${successShare}%` }}
                      />
                    </div>
                    <div className="trend-label">{label}</div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </section>

      <section className="remediation-section">
        <div className="section-header">
          <h2>Verification status</h2>
          <div className="results-count">
            {verificationLoading ? 'Loading…' : `${verification?.total_attempted ?? 0} remediations attempted`}
          </div>
        </div>
        {verificationLoading ? (
          <div className="loading-placeholder">Loading verification stats…</div>
        ) : !verification || verification.total_attempted === 0 ? (
          <div className="empty-state"><p>No remediations have been attempted in this window.</p></div>
        ) : (
          <div className="verification-grid">
            {verificationRows.map((row) => (
              <div key={row.label} className="verification-card" style={{ borderLeftColor: row.color }}>
                <div className="verification-label">{row.label}</div>
                <div className="verification-value">{row.value}</div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="remediation-section">
        <div className="section-header">
          <h2>Pending approvals</h2>
          <div className="results-count">
            {approvalsLoading ? 'Loading…' : `${approvals.length} awaiting decision`}
          </div>
        </div>
        {approvalsLoading ? (
          <div className="loading-placeholder">Loading pending approvals…</div>
        ) : approvals.length === 0 ? (
          <div className="empty-state">
            <p>No high-severity remediations are waiting for approval.</p>
          </div>
        ) : (
          <div className="results-table-container">
            <table className="results-table">
              <thead>
                <tr>
                  <th>Rule</th>
                  <th>Severity</th>
                  <th>Node</th>
                  <th>Created</th>
                  <th>Expires</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {approvals.map((a) => (
                  <tr key={a.id}>
                    <td><code>{a.rule_id}</code></td>
                    <td>
                      <span
                        className="severity-badge"
                        style={{
                          backgroundColor: severityColor(a.severity),
                          color: '#fff',
                          padding: '2px 8px',
                          borderRadius: '4px',
                          fontSize: '0.875rem',
                        }}
                      >
                        {(a.severity || 'unknown').toUpperCase()}
                      </span>
                    </td>
                    <td><code>{a.node_id}</code></td>
                    <td>{formatDate(a.created_at)}</td>
                    <td>{formatDate(a.expires_at)}</td>
                    <td>
                      <div className="approval-actions">
                        <button
                          type="button"
                          className="btn-primary"
                          onClick={() => onApprove(a)}
                          disabled={actionState.inFlightId === a.id}
                          aria-label={`Approve remediation for ${a.rule_id}`}
                        >
                          {actionState.inFlightId === a.id ? 'Working…' : 'Approve'}
                        </button>
                        <button
                          type="button"
                          className="btn-secondary"
                          onClick={() => onDeny(a)}
                          disabled={actionState.inFlightId === a.id}
                          aria-label={`Deny remediation for ${a.rule_id}`}
                        >
                          Deny
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
