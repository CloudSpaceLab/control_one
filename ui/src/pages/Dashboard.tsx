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
import { Alert, Eyebrow, KpiTile, Panel } from '../components/kit';
import { Button } from '../components/ui/button';

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
    <div className="flex flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8">
      {/* Executive Risk Dashboard */}
      <div className="flex flex-col gap-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <Eyebrow>Executive Risk Dashboard</Eyebrow>
            <h2 className="mt-0.5 font-display text-xl font-semibold text-foreground">
              Security Posture at a Glance
            </h2>
            <p className="mt-1 text-sm text-text-muted">
              {executiveLoading ? 'Loading executive metrics…' : 'Real-time risk visibility and operational metrics'}
            </p>
          </div>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => { refresh(); refreshExecutiveMetrics(); }}
            disabled={executiveLoading || loading}
          >
            {executiveLoading || loading ? 'Refreshing…' : 'Refresh'}
          </Button>
        </div>

        {error && (
          <Alert
            variant="critical"
            actions={
              <Button variant="ghost" size="sm" onClick={() => { refresh(); refreshExecutiveMetrics(); }}>
                Retry
              </Button>
            }
          >
            {error}
          </Alert>
        )}

        {/* Executive Metrics Grid */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_2fr]">
          {/* Left Column - Risk Score */}
          <div>
            <RiskScoreCard score={riskScore} loading={executiveLoading} />
          </div>

          {/* Right Column - Metrics Row + Aging */}
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
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
            <AgingTable aging={criticalAging} loading={executiveLoading} />
          </div>
        </div>
      </div>

      <hr className="border-border-subtle" />

      {/* Operational Section */}
      <div className="flex flex-col gap-4">
        <div>
          <Eyebrow>Operational Details</Eyebrow>
          <h3 className="mt-0.5 font-display text-lg font-semibold text-foreground">
            Infrastructure Overview
          </h3>
          <p className="mt-1 text-sm text-text-muted">
            {loading
              ? 'Loading operational data…'
              : `Last updated ${new Date(overview.generated_at || Date.now()).toLocaleTimeString()}`}
          </p>
        </div>

        <div className="grid grid-cols-2 gap-4 lg:grid-cols-3">
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
            sub={
              Object.entries(overview.rule_trigger_counts_24h ?? {})
                .map(([k, v]) => `${k}:${v}`)
                .join(' · ') || '—'
            }
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

        <FleetTopologyCard
          tenantId={tenantId}
          onNodeClick={(n) => navigate(`/nodes?focus=${encodeURIComponent(n.id)}`)}
        />

        {/* Quick Actions */}
        <Panel padding="md" title="Quick actions">
          <ul className="flex flex-col gap-3">
            <li className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-foreground">Author a rule</p>
                <p className="text-xs text-text-muted">Compliance, port, or log rule — rolls out in realtime.</p>
              </div>
              <Button type="button" variant="primary" size="sm" onClick={() => navigate('/rules')}>
                Go
              </Button>
            </li>
            <li className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-foreground">Register hypervisor</p>
                <p className="text-xs text-text-muted">Add a KVM / VMware / AWS / Azure host to provision from.</p>
              </div>
              <Button type="button" variant="primary" size="sm" onClick={() => navigate('/hypervisors')}>
                Go
              </Button>
            </li>
            <li className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-foreground">Enroll a node</p>
                <p className="text-xs text-text-muted">Run the one-line installer or bulk-enroll via SSH.</p>
              </div>
              <Button type="button" variant="primary" size="sm" onClick={() => navigate('/nodes')}>
                Go
              </Button>
            </li>
          </ul>
        </Panel>
      </div>
    </div>
  );
}

function SeverityCard({ title, breakdown }: { title: string; breakdown: SeverityBreakdown }): JSX.Element {
  return (
    <KpiTile
      label={title}
      value={breakdown.total}
      size="sm"
      hint={
        <span className="flex gap-1.5 flex-wrap">
          <span className="text-state-critical">{breakdown.critical}</span>
          <span className="text-text-muted">crit ·</span>
          <span className="text-state-warning">{breakdown.high}</span>
          <span className="text-text-muted">high ·</span>
          <span className="text-yellow-400">{breakdown.medium}</span>
          <span className="text-text-muted">med ·</span>
          <span className="text-state-healthy">{breakdown.low}</span>
          <span className="text-text-muted">low</span>
        </span>
      }
    />
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
    <div
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => { if (e.key === 'Enter' || e.key === ' ') onClick(); } : undefined}
      className={onClick ? 'cursor-pointer' : undefined}
    >
      <KpiTile label={title} value={total} size="sm" hint={sub} />
    </div>
  );
}

// FleetTopologyCard — every node as a colour dot. Tap to drill in. Adapts
// from 5 nodes to thousands without code changes; --state-* tokens drive
// the colour and the pulse on critical so accessibility wins for free.
function FleetTopologyCard({
  tenantId,
  onNodeClick,
}: {
  tenantId?: string;
  onNodeClick: (n: TopologyNode) => void;
}) {
  const { data, loading, error } = useFleetSummary({ tenantId, intervalMs: 30000 });

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
    <Panel padding="md">
      <header className="flex items-center justify-between gap-3">
        <div>
          <Eyebrow>Fleet topology</Eyebrow>
          <h3 className="mt-0.5 font-display text-base font-semibold text-foreground">
            {totals.nodes} nodes · live
          </h3>
        </div>
        <div className="flex flex-wrap gap-3 text-xs">
          <Legend label="Healthy" state="healthy" count={totals.healthy} />
          <Legend label="Warning" state="warning" count={totals.warning} />
          <Legend label="Degraded" state="degraded" count={totals.degraded} />
          <Legend label="Critical" state="critical" count={totals.critical} />
          <Legend label="Unknown" state="unknown" count={totals.unknown} />
        </div>
      </header>
      {error ? (
        <p className="text-xs text-state-critical">Topology offline: {error.message}</p>
      ) : null}
      {loading ? <p className="text-xs text-text-muted">Syncing…</p> : null}
      <TopologyGrid nodes={nodes} onNodeClick={onNodeClick} />
      {data?.source === 'postgres-fallback' ? (
        <p className="mt-1 text-xs text-state-warning">
          Fast view — Doris unavailable, sourced from Postgres rollups.
        </p>
      ) : null}
    </Panel>
  );
}

function Legend({ label, state, count }: { label: string; state: State; count: number }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusDot state={state} />
      <span className="text-text-muted">{label}</span>
      <strong>{count}</strong>
    </span>
  );
}
