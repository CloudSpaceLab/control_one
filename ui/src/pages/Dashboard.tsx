import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import type { DashboardOverview, SeverityBreakdown } from '../lib/api';
import { useFleetSummary } from '../hooks/useFleetSummary';
import TopologyGrid, { TopologyNode } from '../components/glyphs/TopologyGrid';
import StatusDot, { State } from '../components/glyphs/StatusDot';
import {
  RiskScoreCard,
  MetricTrend,
  AgingTable,
  type RiskScore,
  type MTTDMetrics,
  type MTTRMetrics,
  type RemediationVelocity,
  type FindingAging,
} from '../components/executive';
import './Dashboard.css';

const REFRESH_TOPICS = [
  'security.event',
  'health.incident',
  'rule.triggered',
  'remediation.applied',
  'compliance.fired',
  'alert.opened',
];

const INITIAL_SEV: SeverityBreakdown = { critical: 0, high: 0, medium: 0, low: 0, total: 0 };

const INITIAL_OVERVIEW: DashboardOverview = {
  generated_at: '',
  node_counts: { total: 0, healthy: 0, offline: 0 },
  security_event_counts: INITIAL_SEV,
  health_incident_counts: INITIAL_SEV,
  compliance_summary: { total: 0, passed: 0, failed: 0 },
  rule_trigger_counts_24h: {},
  remediations_applied_24h: 0,
};

export function Dashboard(): JSX.Element {
  const navigate = useNavigate();
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 1, offset: 0 });
  const tenantId = tenants[0]?.id;

  const [overview, setOverview] = useState<DashboardOverview>(INITIAL_OVERVIEW);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Executive Risk Dashboard State
  const [riskScore, setRiskScore] = useState<RiskScore | null>(null);
  const [mttdMetrics, setMttdMetrics] = useState<MTTDMetrics | null>(null);
  const [mttrMetrics, setMttrMetrics] = useState<MTTRMetrics | null>(null);
  const [remediationVelocity, setRemediationVelocity] = useState<RemediationVelocity | null>(null);
  const [criticalAging, setCriticalAging] = useState<FindingAging | null>(null);
  const [executiveLoading, setExecutiveLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const data = await client.getDashboardOverview(tenantId);
      setOverview(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load dashboard');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  // Executive metrics refresh
  const refreshExecutiveMetrics = useCallback(async () => {
    if (!tenantId) return;
    
    setExecutiveLoading(true);
    try {
      // Fetch all executive metrics in parallel using public API methods
      const [risk, mttd, mttr, velocity, aging] = await Promise.all([
        client.getRiskScore(tenantId),
        client.getMTTDMetrics(tenantId, 'critical', 7),
        client.getMTTRMetrics(tenantId, 'critical', 7),
        client.getRemediationVelocity(tenantId, 30),
        client.getFindingsAging(tenantId, 'critical'),
      ]);

      setRiskScore(risk);
      setMttdMetrics(mttd);
      setMttrMetrics(mttr);
      setRemediationVelocity(velocity);
      setCriticalAging(aging);
    } catch (err) {
      console.error('Failed to load executive metrics:', err);
    } finally {
      setExecutiveLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (cancelled) return;
      await refresh();
      await refreshExecutiveMetrics();
    })();
    const poll = window.setInterval(() => {
      if (!cancelled) {
        refresh();
        refreshExecutiveMetrics();
      }
    }, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(poll);
    };
  }, [refresh, refreshExecutiveMetrics]);

  // Realtime refresh on incoming events.
  useEventStream(tenantId, REFRESH_TOPICS, () => {
    refresh();
  });

  const totalRuleTriggers = useMemo(
    () => Object.values(overview.rule_trigger_counts_24h ?? {}).reduce((a, b) => a + b, 0),
    [overview.rule_trigger_counts_24h],
  );

  return (
    <section className="dashboard-root">
      <div className="dashboard-container">
        {/* Executive Risk Dashboard */}
        <div className="executive-section">
          <header className="executive-header">
            <div>
              <p className="executive-eyebrow">Executive Risk Dashboard</p>
              <h2 className="executive-title">Security Posture at a Glance</h2>
              <p className="executive-subtitle">
                {executiveLoading ? 'Loading executive metrics…' : 'Real-time risk visibility and operational metrics'}
              </p>
            </div>
            <button 
              type="button" 
              className="refresh-btn" 
              onClick={() => { refresh(); refreshExecutiveMetrics(); }}
              disabled={executiveLoading || loading}
            >
              {executiveLoading || loading ? '⟳ Refreshing…' : '↻ Refresh'}
            </button>
          </header>

          {/* Error State */}
          {error && (
            <div className="error-card" style={{ marginBottom: '24px' }}>
              <svg className="error-icon" viewBox="0 0 20 20" fill="currentColor">
                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clipRule="evenodd" />
              </svg>
              <p className="error-message">{error}</p>
              <button className="error-retry-btn" onClick={() => { refresh(); refreshExecutiveMetrics(); }}>
                Retry
              </button>
            </div>
          )}

          {/* Executive Metrics Grid */}
          <div className="executive-grid">
            {/* Left Column - Risk Score */}
            <div className="executive-metric-large">
              <RiskScoreCard score={riskScore} loading={executiveLoading} />
            </div>

            {/* Right Column - Metrics Row + Aging */}
            <div>
              <div className="executive-metrics-row">
                <MetricTrend
                  title="MTTD (Mean Time to Detect)"
                  value={mttdMetrics?.mean_minutes ?? null}
                  unit=""
                  target={15}
                  trend={mttdMetrics?.mean_minutes ? (mttdMetrics.mean_minutes < 15 ? 'up' : 'down') : undefined}
                  trendValue={5}
                  loading={executiveLoading}
                  format="time"
                />
                <MetricTrend
                  title="MTTR (Mean Time to Remediate)"
                  value={mttrMetrics?.mean_minutes ?? null}
                  unit=""
                  target={240}
                  trend={mttrMetrics?.mean_minutes ? (mttrMetrics.mean_minutes < 240 ? 'up' : 'down') : undefined}
                  trendValue={10}
                  loading={executiveLoading}
                  format="time"
                />
                <MetricTrend
                  title="Remediation Velocity"
                  value={remediationVelocity?.remediations ?? null}
                  unit="findings"
                  trend={remediationVelocity?.trend_direction}
                  trendValue={remediationVelocity?.trend_percent}
                  loading={executiveLoading}
                  format="number"
                />
              </div>
              <div className="executive-aging-container" style={{ marginTop: '16px' }}>
                <AgingTable aging={criticalAging} loading={executiveLoading} />
              </div>
            </div>
          </div>
        </div>

        {/* Visual Separator */}
        <div className="dashboard-divider" />

        {/* Operational Section */}
        <div className="operational-section">
          <header className="operational-header">
            <div>
              <p className="operational-eyebrow">Operational Details</p>
              <h3 className="operational-title">Infrastructure Overview</h3>
              <p className="operational-subtitle">
                {loading ? 'Loading operational data…' : `Last updated ${new Date(overview.generated_at || Date.now()).toLocaleTimeString()}`}
              </p>
            </div>
          </header>

      <div className="stat-grid">
        <SeverityCard title="Security events (24h)" breakdown={overview.security_event_counts} />
        <SeverityCard title="Open health incidents" breakdown={overview.health_incident_counts} />
        <CountCard
          title="Compliance alerts (24h)"
          total={overview.compliance_summary.failed}
          sub={`${overview.compliance_summary.passed} passed / ${overview.compliance_summary.total} total`}
          onClick={() => navigate('/compliance')}
        />
        <CountCard
          title="Rule triggers (24h)"
          total={totalRuleTriggers}
          sub={Object.entries(overview.rule_trigger_counts_24h ?? {})
            .map(([k, v]) => `${k}:${v}`)
            .join(' · ') || '—'}
          onClick={() => navigate('/rules')}
        />
        <CountCard
          title="Auto-remediations (24h)"
          total={overview.remediations_applied_24h}
          sub="Safety gates active"
        />
        <CountCard
          title="Nodes"
          total={overview.node_counts.total}
          sub={`${overview.node_counts.healthy} healthy · ${overview.node_counts.offline} offline`}
          onClick={() => navigate('/nodes')}
        />
      </div>

      <FleetTopologyCard onNodeClick={(n) => navigate(`/nodes?focus=${encodeURIComponent(n.id)}`)} />

      <div className="dashboard-panels">
        <article className="quick-actions">
          <h3>Quick actions</h3>
          <ul>
            <li>
              <div>
                <strong>Author a rule</strong>
                <p>Compliance, port, or log rule — rolls out in realtime.</p>
              </div>
              <button type="button" className="primary-button" onClick={() => navigate('/rules')}>
                Go
              </button>
            </li>
            <li>
              <div>
                <strong>Register hypervisor</strong>
                <p>Add a KVM / VMware / AWS / Azure host to provision from.</p>
              </div>
              <button type="button" className="primary-button" onClick={() => navigate('/hypervisors')}>
                Go
              </button>
            </li>
            <li>
              <div>
                <strong>Enroll a node</strong>
                <p>Run the one-line installer or bulk-enroll via SSH.</p>
              </div>
              <button type="button" className="primary-button" onClick={() => navigate('/nodes')}>
                Go
              </button>
            </li>
          </ul>
        </article>
      </div>
        </div>
      </div>
    </section>
  );
}

function SeverityCard({ title, breakdown }: { title: string; breakdown: SeverityBreakdown }): JSX.Element {
  return (
    <article className="stat-card">
      <p>{title}</p>
      <span className="stat-value">{breakdown.total}</span>
      <small>
        <span className="crit">{breakdown.critical}</span> crit ·{' '}
        <span className="high">{breakdown.high}</span> high ·{' '}
        <span className="med">{breakdown.medium}</span> med ·{' '}
        <span className="low">{breakdown.low}</span> low
      </small>
    </article>
  );
}

function CountCard({
  title,
  total,
  sub,
  onClick,
}: {
  title: string;
  total: number;
  sub: string;
  onClick?: () => void;
}): JSX.Element {
  return (
    <article
      className="stat-card"
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => { if (e.key === 'Enter' || e.key === ' ') onClick(); } : undefined}
      style={onClick ? { cursor: 'pointer' } : undefined}
    >
      <p>{title}</p>
      <span className="stat-value">{total}</span>
      <small>{sub}</small>
    </article>
  );
}

// FleetTopologyCard — every node as a colour dot. Tap to drill in. Adapts
// from 5 nodes to thousands without code changes; --state-* tokens drive
// the colour and the pulse on critical so accessibility wins for free.
function FleetTopologyCard({ onNodeClick }: { onNodeClick: (n: TopologyNode) => void }) {
  const { data, loading, error } = useFleetSummary({ intervalMs: 30000 });

  const nodes: TopologyNode[] = (data?.nodes ?? []).map((n) => ({
    id: n.node_id,
    hostname: n.hostname,
    state: (n.state ?? 'unknown') as State,
    hint: `${n.hostname ?? n.node_id} · cpu ${Math.round((n.cpu_p95 ?? 0) * 100)}% · mem ${Math.round(
      (n.mem_p95 ?? 0) * 100,
    )}% · ${n.conn_count ?? 0} conns · ${n.alerts_open ?? 0} alerts`,
  }));

  const totals = data?.totals ?? {
    nodes: 0, healthy: 0, warning: 0, degraded: 0, critical: 0, unknown: 0,
  };

  return (
    <article className="card" style={{ padding: 16, marginTop: 24 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
        <div>
          <p className="eyebrow">Fleet topology</p>
          <h3 style={{ marginTop: 4 }}>{totals.nodes} nodes · live</h3>
        </div>
        <div style={{ display: 'flex', gap: 12, fontSize: 12 }}>
          <Legend label="Healthy" state="healthy" count={totals.healthy} />
          <Legend label="Warning" state="warning" count={totals.warning} />
          <Legend label="Degraded" state="degraded" count={totals.degraded} />
          <Legend label="Critical" state="critical" count={totals.critical} />
          <Legend label="Unknown" state="unknown" count={totals.unknown} />
        </div>
      </header>
      {error ? (
        <p style={{ color: 'var(--state-critical)', fontSize: 13 }}>Topology offline: {error.message}</p>
      ) : null}
      {loading ? <p style={{ color: 'var(--text-secondary)', fontSize: 13 }}>Syncing…</p> : null}
      <TopologyGrid nodes={nodes} onNodeClick={onNodeClick} />
      {data?.source === 'postgres-fallback' ? (
        <small style={{ color: 'var(--state-warning)', display: 'block', marginTop: 8 }}>
          Fast view — Doris unavailable, sourced from Postgres rollups.
        </small>
      ) : null}
    </article>
  );
}

function Legend({ label, state, count }: { label: string; state: State; count: number }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      <StatusDot state={state} />
      <span style={{ color: 'var(--text-secondary)' }}>{label}</span>
      <strong>{count}</strong>
    </span>
  );
}

