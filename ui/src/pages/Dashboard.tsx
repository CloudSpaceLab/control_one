import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import type { DashboardOverview, SeverityBreakdown } from '../lib/api';
import { useFleetSummary } from '../hooks/useFleetSummary';
import TopologyGrid, { TopologyNode } from '../components/glyphs/TopologyGrid';
import StatusDot, { State } from '../components/glyphs/StatusDot';

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

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (cancelled) return;
      await refresh();
    })();
    const poll = window.setInterval(() => {
      if (!cancelled) refresh();
    }, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(poll);
    };
  }, [refresh]);

  // Realtime refresh on incoming events.
  useEventStream(tenantId, REFRESH_TOPICS, () => {
    refresh();
  });

  const totalRuleTriggers = useMemo(
    () => Object.values(overview.rule_trigger_counts_24h ?? {}).reduce((a, b) => a + b, 0),
    [overview.rule_trigger_counts_24h],
  );

  return (
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Last 24 hours</p>
          <h2>Your infrastructure at a glance</h2>
          <p className="subtitle">
            {loading ? 'Loading…' : `Updated ${new Date(overview.generated_at || Date.now()).toLocaleTimeString()}`}
          </p>
        </div>
        <button type="button" className="primary-button" onClick={refresh}>
          Refresh
        </button>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

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
    </section>
  );
}

function SeverityCard({ title, breakdown }: { title: string; breakdown: SeverityBreakdown }): JSX.Element {
  return (
    <article className="stat-card">
      <p>{title}</p>
      <span className="stat-value">{breakdown.total}</span>
      <small>
        {breakdown.critical} crit · {breakdown.high} high · {breakdown.medium} med · {breakdown.low} low
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
      {loading && nodes.length === 0 ? (
        <p style={{ color: 'var(--text-secondary)', fontSize: 13 }}>Loading fleet…</p>
      ) : (
        <TopologyGrid nodes={nodes} onNodeClick={onNodeClick} />
      )}
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
