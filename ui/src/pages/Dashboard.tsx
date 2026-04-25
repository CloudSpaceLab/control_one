import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenants } from '../hooks/useTenants';
import type { DashboardOverview, SeverityBreakdown } from '../lib/api';

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
