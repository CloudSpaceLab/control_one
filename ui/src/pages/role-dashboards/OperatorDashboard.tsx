import { useQuery } from '@tanstack/react-query';
import { CheckCircle2, Plus, RefreshCw } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import {
  Chart,
  CommandSection,
  DataTable,
  DEFAULT_TIME_RANGES,
  EmptyState,
  KpiTile,
  OnboardingChecklist,
  Panel,
  SectionHeader,
  StatusTag,
  TimeRangePills,
} from '@/components/kit';
import { DashboardGrid, DashboardGridItem } from '@/components/shell';
import { useApiClient } from '@/hooks/useApiClient';
import { useLiveSubscribe } from '@/hooks/useLiveSubscribe';
import { useOnboardingState } from '@/hooks/useOnboardingState';
import { useTenant } from '@/providers/TenantProvider';
import type {
  Alert,
  DashboardOverview,
  MTTDMetrics,
  MTTRMetrics,
  RiskScore,
  SecurityEventSeriesPoint,
} from '@/lib/api';
import type { StateTone } from '@/components/kit';
import type { ColumnDef } from '@tanstack/react-table';

const SEVERITY_TONE: Record<string, StateTone> = {
  critical: 'critical',
  high: 'degraded',
  medium: 'warning',
  low: 'info',
  info: 'info',
};

const PERIOD_DAYS: Record<string, number> = { '24h': 1, '7d': 7, '30d': 30 };

function riskTone(score?: number): StateTone | 'brand' {
  if (score == null) return 'brand';
  if (score >= 80) return 'healthy';
  if (score >= 60) return 'warning';
  return 'critical';
}

function complianceTone(pct?: number): StateTone | 'brand' {
  if (pct == null) return 'brand';
  if (pct >= 95) return 'healthy';
  if (pct >= 80) return 'warning';
  return 'critical';
}

function formatMinutes(min: number): string {
  if (!isFinite(min) || min <= 0) return '—';
  if (min < 1) return `${(min * 60).toFixed(0)}s`;
  if (min < 60) return `${min.toFixed(0)}m`;
  const h = Math.floor(min / 60);
  const m = Math.round(min % 60);
  return m === 0 ? `${h}h` : `${h}h ${m}m`;
}

function relTime(ts?: string): string {
  if (!ts) return '—';
  const diff = Date.now() - new Date(ts).getTime();
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
  return `${Math.round(diff / 86_400_000)}d ago`;
}

function secSeriesSparkline(
  series: SecurityEventSeriesPoint[] | undefined,
  field: 'critical' | 'high' | 'total',
): number[] {
  if (!series || series.length === 0) return [];
  return series.map((p) => p[field]);
}

export function OperatorDashboard(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [period, setPeriod] = useState('24h');
  const days = PERIOD_DAYS[period] ?? 1;

  // Last-refreshed counter
  const [lastRefreshed, setLastRefreshed] = useState<Date>(new Date());
  const [sinceLabel, setSinceLabel] = useState('just now');
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const tickLabel = () => {
    const diff = Date.now() - lastRefreshed.getTime();
    if (diff < 60_000) setSinceLabel(`${Math.round(diff / 1_000)}s ago`);
    else setSinceLabel(`${Math.round(diff / 60_000)}m ago`);
  };

  useEffect(() => {
    if (intervalRef.current) clearInterval(intervalRef.current);
    intervalRef.current = setInterval(tickLabel, 5_000);
    return () => { if (intervalRef.current) clearInterval(intervalRef.current); };
  }, [lastRefreshed]);

  const overviewQ = useQuery<DashboardOverview>({
    queryKey: ['dashboard.overview', currentTenantId, period],
    queryFn: () => client.getDashboardOverview(currentTenantId ?? undefined, period),
    refetchInterval: 30_000,
  });

  useEffect(() => {
    if (overviewQ.dataUpdatedAt) setLastRefreshed(new Date(overviewQ.dataUpdatedAt));
  }, [overviewQ.dataUpdatedAt]);

  const alertsQ = useQuery({
    queryKey: ['alerts.open', currentTenantId],
    queryFn: () => client.listAlerts({ tenantId: currentTenantId ?? undefined, state: 'open', limit: 50, offset: 0 }),
    refetchInterval: 30_000,
  });

  const riskQ = useQuery<RiskScore>({
    queryKey: ['risk-score', currentTenantId],
    queryFn: () => client.getRiskScore(currentTenantId ?? undefined),
    refetchInterval: 60_000,
  });

  const mttdQ = useQuery<MTTDMetrics>({
    queryKey: ['mttd', currentTenantId, days],
    queryFn: () => client.getMTTDMetrics(currentTenantId ?? undefined, 'critical', days),
  });

  const mttrQ = useQuery<MTTRMetrics>({
    queryKey: ['mttr', currentTenantId, days],
    queryFn: () => client.getMTTRMetrics(currentTenantId ?? undefined, 'critical', days),
  });

  const live = useLiveSubscribe(currentTenantId ?? undefined, [
    { topic: 'alert.opened', invalidate: [['alerts.open'], ['dashboard.overview']] },
    { topic: 'rule.triggered', invalidate: [['dashboard.overview']] },
    { topic: 'remediation.applied', invalidate: [['dashboard.overview']] },
    { topic: 'health.incident', invalidate: [['dashboard.overview']] },
  ]);

  const ov = overviewQ.data;
  const loading = overviewQ.isLoading;

  const totalRuleTriggers = Object.values(ov?.rule_trigger_counts_24h ?? {}).reduce((a, b) => a + b, 0);
  const ruleBreakdown = Object.entries(ov?.rule_trigger_counts_24h ?? {})
    .sort(([, a], [, b]) => b - a)
    .slice(0, 6);

  const allClear =
    !loading &&
    (ov?.security_event_counts.critical ?? 0) === 0 &&
    (alertsQ.data?.data.length ?? 0) === 0;

  // Build sparklines from series
  const critSpark = secSeriesSparkline(ov?.security_event_series, 'critical');
  const highSpark = secSeriesSparkline(ov?.security_event_series, 'high');
  const totalSpark = secSeriesSparkline(ov?.security_event_series, 'total');

  // Chart data for security events stacked bar
  const chartLabels = (ov?.security_event_series ?? []).map((p) =>
    period === '24h' ? p.ts.slice(11, 16) : p.ts.slice(5, 10),
  );
  const hasChartData = (ov?.security_event_series?.length ?? 0) > 1;

  const alertColumns: ColumnDef<Alert>[] = [
    {
      accessorKey: 'severity',
      header: 'Sev',
      cell: ({ getValue }) => {
        const sev = (getValue() as string) || 'info';
        return <StatusTag tone={SEVERITY_TONE[sev] ?? 'info'}>{sev.toUpperCase()}</StatusTag>;
      },
    },
    {
      accessorKey: 'title',
      header: 'Alert',
      cell: ({ row }) => (
        <div className="flex min-w-0 flex-col">
          <span className="truncate text-sm text-foreground">{row.original.title}</span>
          {row.original.summary && (
            <span className="truncate text-xs text-text-muted">{row.original.summary}</span>
          )}
        </div>
      ),
    },
    {
      accessorKey: 'source',
      header: 'Source',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs text-text-secondary">{getValue() as string}</span>
      ),
    },
    {
      accessorKey: 'opened_at',
      header: 'Opened',
      cell: ({ getValue }) => (
        <span className="text-xs text-text-secondary">{relTime(getValue() as string)}</span>
      ),
    },
  ];

  const onboarding = useOnboardingState();

  if (onboarding.isReady && onboarding.isEmpty) {
    return (
      <div className="flex flex-col gap-6">
        <SectionHeader
          eyebrow="WELCOME · OPERATOR ONBOARDING"
          title="Onboard your first server to start running rules"
          description="Operator deck unlocks once you've added a host and enabled at least one rule."
        />
        <OnboardingChecklist steps={onboarding.steps} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {!onboarding.steps.every((s) => s.done || !s.required) && (
        <OnboardingChecklist
          eyebrow="GET STARTED"
          title="Finish wiring up your fleet"
          description="A handful of required steps remain."
          steps={onboarding.steps}
        />
      )}

      <SectionHeader
        eyebrow="COMMAND CENTER"
        title="Operations"
        description="Security, compliance, and response posture across the fleet."
        actions={
          <>
            <span className="flex items-center gap-1.5 text-xs text-text-muted">
              <RefreshCw className="h-3 w-3" />
              {sinceLabel}
            </span>
            <TimeRangePills value={period} options={DEFAULT_TIME_RANGES} onChange={setPeriod} />
            <Button asChild variant="primary" shimmer>
              <Link to="/rules">
                <Plus className="h-4 w-4" /> New rule
              </Link>
            </Button>
          </>
        }
      />

      {allClear && (
        <Panel tone="glow" padding="md" toneAccent="healthy" eyebrow="ALL CLEAR" title="No critical alerts">
          <div className="flex items-center gap-3 text-sm text-text-secondary">
            <CheckCircle2 className="h-5 w-5 text-state-healthy" />
            All operator queues are empty. Live stream is{' '}
            <span className="font-mono text-state-healthy">{live.state}</span>.
          </div>
        </Panel>
      )}

      {/* ── SECURITY ─────────────────────────────── */}
      <CommandSection label="SECURITY" tone="critical">
        <KpiTile
          label="CRITICAL EVENTS"
          value={ov?.security_event_delta?.current ?? ov?.security_event_counts.critical ?? '—'}
          delta={ov?.security_event_delta?.delta_pct}
          invertDelta
          sparkline={critSpark.length > 1 ? critSpark : undefined}
          tone="critical"
          loading={loading}
        />
        <KpiTile
          label="HIGH EVENTS"
          value={ov?.security_event_counts.high ?? '—'}
          invertDelta
          sparkline={highSpark.length > 1 ? highSpark : undefined}
          tone="degraded"
          loading={loading}
        />
        <KpiTile
          label="RULE TRIGGERS"
          value={ov?.rule_trigger_delta?.current ?? totalRuleTriggers}
          delta={ov?.rule_trigger_delta?.delta_pct}
          invertDelta
          sparkline={totalSpark.length > 1 ? totalSpark : undefined}
          tone="warning"
          loading={loading}
        />
        <KpiTile
          label="REMEDIATIONS"
          value={ov?.remediation_delta?.current ?? ov?.remediations_applied_24h ?? '—'}
          delta={ov?.remediation_delta?.delta_pct}
          tone="healthy"
          loading={loading}
        />
      </CommandSection>

      {hasChartData && (
        <Panel padding="md" eyebrow={`SECURITY EVENTS · ${period.toUpperCase()}`} title="Event volume" toneAccent="critical">
          <Chart
            kind="bar"
            height={180}
            ariaLabel="Security events over time"
            data={{
              labels: chartLabels,
              datasets: [
                {
                  label: 'Critical',
                  data: (ov?.security_event_series ?? []).map((p) => p.critical),
                  backgroundColor: 'rgba(239,68,68,0.75)',
                  borderColor: 'rgba(239,68,68,1)',
                  borderWidth: 1,
                },
                {
                  label: 'High',
                  data: (ov?.security_event_series ?? []).map((p) => p.high),
                  backgroundColor: 'rgba(245,158,11,0.65)',
                  borderColor: 'rgba(245,158,11,1)',
                  borderWidth: 1,
                },
                {
                  label: 'Other',
                  data: (ov?.security_event_series ?? []).map((p) => Math.max(0, p.total - p.critical - p.high)),
                  backgroundColor: 'rgba(99,102,241,0.45)',
                  borderColor: 'rgba(99,102,241,0.8)',
                  borderWidth: 1,
                },
              ],
            }}
            options={{ scales: { x: { stacked: true }, y: { stacked: true, beginAtZero: true } } }}
          />
        </Panel>
      )}

      {/* ── COMPLIANCE & POSTURE ─────────────────── */}
      <CommandSection label="COMPLIANCE & POSTURE" tone="warning">
        <KpiTile
          label="PASS RATE"
          value={ov?.compliance_pass_rate != null ? `${ov.compliance_pass_rate.toFixed(1)}%` : '—'}
          delta={ov?.compliance_pass_delta?.delta_pct}
          sparkline={
            (ov?.compliance_series ?? []).length > 1
              ? ov!.compliance_series!.map((p) => p.pass_rate)
              : undefined
          }
          tone={complianceTone(ov?.compliance_pass_rate)}
          loading={loading}
        />
        <KpiTile
          label="FAILED CONTROLS"
          value={ov?.compliance_summary.failed ?? '—'}
          invertDelta
          tone="warning"
          loading={loading}
        />
        <KpiTile
          label="HEALTH INCIDENTS"
          value={ov?.health_incident_counts.total ?? '—'}
          invertDelta
          tone={
            (ov?.health_incident_counts.critical ?? 0) > 0
              ? 'critical'
              : (ov?.health_incident_counts.high ?? 0) > 0
              ? 'warning'
              : 'healthy'
          }
          hint={`crit ${ov?.health_incident_counts.critical ?? 0} · high ${ov?.health_incident_counts.high ?? 0}`}
          loading={loading}
        />
        <KpiTile
          label="RISK SCORE"
          value={riskQ.data?.score ?? '—'}
          delta={riskQ.data?.trend_delta}
          tone={riskTone(riskQ.data?.score)}
          hint={riskQ.data ? `${riskQ.data.percent.toFixed(0)}%` : undefined}
          loading={riskQ.isLoading}
        />
      </CommandSection>

      {/* ── RESPONSE ────────────────────────────── */}
      <CommandSection label="RESPONSE" tone="brand">
        <KpiTile
          label="OPEN ALERTS"
          value={alertsQ.data?.data.length ?? '—'}
          invertDelta
          tone="critical"
          loading={alertsQ.isLoading}
        />
        <KpiTile
          label={`MTTD · CRITICAL`}
          value={mttdQ.data ? formatMinutes(mttdQ.data.mean_minutes) : '—'}
          invertDelta
          tone={mttdQ.data && mttdQ.data.mean_minutes < 15 ? 'healthy' : 'warning'}
          hint={mttdQ.data ? `p95 ${formatMinutes(mttdQ.data.p95_minutes)}` : undefined}
          loading={mttdQ.isLoading}
        />
        <KpiTile
          label={`MTTR · CRITICAL`}
          value={mttrQ.data ? formatMinutes(mttrQ.data.mean_minutes) : '—'}
          invertDelta
          tone={mttrQ.data && mttrQ.data.mean_minutes < 240 ? 'healthy' : 'warning'}
          hint={mttrQ.data ? `p95 ${formatMinutes(mttrQ.data.p95_minutes)}` : undefined}
          loading={mttrQ.isLoading}
        />
        <KpiTile
          label="NODES ONLINE"
          value={ov?.node_counts.healthy ?? '—'}
          tone="healthy"
          hint={`${ov?.node_counts.offline ?? 0} offline`}
          loading={loading}
        />
      </CommandSection>

      {/* ── Alert queue + rule activity ─────────── */}
      <DashboardGrid>
        <DashboardGridItem span={{ base: 12, lg: 8 }}>
          <Panel padding="md" eyebrow="ALERT QUEUE" title="Open alerts" toneAccent="critical">
            <DataTable
              columns={alertColumns}
              rows={alertsQ.data?.data ?? []}
              rowKey={(r) => r.id}
              loading={alertsQ.isLoading}
              compact
              empty={
                <EmptyState
                  tone="success"
                  icon={<CheckCircle2 />}
                  title="No open alerts"
                  description="Operator deck is clear."
                />
              }
            />
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 4 }}>
          <Panel padding="md" eyebrow="RULE ACTIVITY · 24H" title="Top firing rules" toneAccent="warning">
            {ruleBreakdown.length === 0 ? (
              <EmptyState title="No rule triggers" description="Once a rule fires, it'll show here." />
            ) : (
              <ul className="flex flex-col gap-2">
                {ruleBreakdown.map(([rule, count]) => (
                  <li
                    key={rule}
                    className="flex items-center justify-between rounded-md border border-border-subtle bg-surface px-3 py-2"
                  >
                    <span className="truncate text-sm text-foreground">{rule}</span>
                    <span className="font-mono text-xs tabular-nums text-state-warning">{count}</span>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </DashboardGridItem>
      </DashboardGrid>
    </div>
  );
}
