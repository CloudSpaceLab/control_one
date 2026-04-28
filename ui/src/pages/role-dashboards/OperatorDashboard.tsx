import { useQuery } from '@tanstack/react-query';
import { CheckCircle2, Plus, ShieldAlert, Workflow } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import {
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
import type { Alert, DashboardOverview } from '@/lib/api';
import type { StateTone } from '@/components/kit';
import type { ColumnDef } from '@tanstack/react-table';

const SEVERITY_TONE: Record<string, StateTone> = {
  critical: 'critical',
  high: 'degraded',
  medium: 'warning',
  low: 'info',
  info: 'info',
};

export function OperatorDashboard(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [period, setPeriod] = useState('24h');

  const overviewQ = useQuery<DashboardOverview>({
    queryKey: ['dashboard.overview', currentTenantId],
    queryFn: () => client.getDashboardOverview(currentTenantId ?? undefined),
    refetchInterval: 30_000,
  });

  const alertsQ = useQuery({
    queryKey: ['alerts.open', currentTenantId],
    queryFn: () => client.listAlerts({ tenantId: currentTenantId ?? undefined, state: 'open', limit: 50, offset: 0 }),
    refetchInterval: 30_000,
  });

  const live = useLiveSubscribe(currentTenantId ?? undefined, [
    { topic: 'alert.opened', invalidate: [['alerts.open'], ['dashboard.overview']] },
    { topic: 'rule.triggered', invalidate: [['dashboard.overview']] },
    { topic: 'remediation.applied', invalidate: [['dashboard.overview']] },
    { topic: 'health.incident', invalidate: [['dashboard.overview']] },
  ]);

  const totalRuleTriggers = Object.values(overviewQ.data?.rule_trigger_counts_24h ?? {}).reduce(
    (a, b) => a + b,
    0,
  );
  const ruleBreakdown = Object.entries(overviewQ.data?.rule_trigger_counts_24h ?? {})
    .sort(([, a], [, b]) => b - a)
    .slice(0, 6);

  const allClear =
    !overviewQ.isLoading &&
    (overviewQ.data?.security_event_counts.critical ?? 0) === 0 &&
    (alertsQ.data?.data.length ?? 0) === 0;

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
          description="Operator deck unlocks once you've added a host and enabled at least one rule. Use the checklist below."
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
          description="A handful of required steps remain — finish them to unlock realtime detection and remediation."
          steps={onboarding.steps}
        />
      )}
      <SectionHeader
        eyebrow="OPERATIONS DECK"
        title="What needs your attention"
        description="Open queues, firing rules, and remediations across the fleet."
        actions={
          <>
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
            All operator queues are empty. Sit tight; live stream is{' '}
            <span className="font-mono text-state-healthy">{live.state}</span>.
          </div>
        </Panel>
      )}

      <DashboardGrid>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="OPEN ALERTS"
            value={alertsQ.data?.data.length ?? '—'}
            tone="critical"
            icon={<ShieldAlert />}
            hint={`crit ${overviewQ.data?.security_event_counts.critical ?? 0} · high ${overviewQ.data?.security_event_counts.high ?? 0}`}
            loading={alertsQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="HEALTH INCIDENTS"
            value={overviewQ.data?.health_incident_counts.total ?? '—'}
            tone={
              (overviewQ.data?.health_incident_counts.critical ?? 0) > 0 ? 'critical' :
              (overviewQ.data?.health_incident_counts.high ?? 0) > 0 ? 'warning' : 'healthy'
            }
            hint={`crit ${overviewQ.data?.health_incident_counts.critical ?? 0} · high ${overviewQ.data?.health_incident_counts.high ?? 0}`}
            loading={overviewQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="RULES FIRING (24H)"
            value={totalRuleTriggers}
            tone="warning"
            hint={`${ruleBreakdown.length} active rules`}
            loading={overviewQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="REMEDIATIONS (24H)"
            value={overviewQ.data?.remediations_applied_24h ?? '—'}
            tone="healthy"
            icon={<Workflow />}
            hint="Safety gates active"
            loading={overviewQ.isLoading}
          />
        </DashboardGridItem>

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
                  description="Operator deck is clear. Live stream stays on."
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

function relTime(ts?: string): string {
  if (!ts) return '—';
  const d = new Date(ts);
  const diff = Date.now() - d.getTime();
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
  return `${Math.round(diff / 86_400_000)}d ago`;
}
