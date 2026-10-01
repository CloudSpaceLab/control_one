import { useState } from 'react';
import { AlertTriangle, RefreshCw, Server, ShieldCheck, ShieldAlert, ListChecks } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  EmptyState,
  Panel,
  SectionHeader,
  TimeRangePills,
  type StateTone,
} from '@/components/kit';
import { useTenant } from '@/providers/TenantProvider';
import {
  AttentionQueue,
} from '@/features/control-room/AttentionQueue';
import { ExecutiveMetricCard } from '@/features/control-room/ExecutiveMetricCard';
import {
  OverviewDetailSheet,
  type ExecutiveDetailKey,
} from '@/features/control-room/OverviewDetailSheet';
import { ProtectionCoverage } from '@/features/control-room/ProtectionCoverage';
import { SecurityActivity } from '@/features/control-room/SecurityActivity';
import {
  CONTROL_ROOM_COPY,
  executiveDescription,
  executiveHeadline,
  formatPeriodDelta,
} from '@/features/control-room/copy';
import { useExecutiveOverview } from '@/features/control-room/useExecutiveOverview';

const CONTROL_ROOM_RANGES = [
  { label: '24H', value: '24h' },
  { label: '7D', value: '7d' },
  { label: '30D', value: '30d' },
];

export function ControlRoom(): JSX.Element {
  const { currentTenantId, currentTenant, loading: tenantLoading } = useTenant();
  const [period, setPeriod] = useState('7d');
  const [detail, setDetail] = useState<ExecutiveDetailKey | null>(null);
  const { overview, loading, error, stale, refresh } = useExecutiveOverview(currentTenantId, period);

  if (!currentTenantId && !tenantLoading) {
    return (
      <EmptyState
        icon={<Server />}
        title="Select a tenant"
        description="Choose a tenant to view the Control Room."
      />
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <SectionHeader
        eyebrow={CONTROL_ROOM_COPY.eyebrow}
        title={overview ? executiveHeadline(overview) : loading ? 'Loading Control Room' : 'Dashboard unavailable'}
        description={
          overview
            ? executiveDescription(overview, currentTenant?.name)
            : currentTenant?.name
              ? `${currentTenant.name} · ${loading ? 'Loading current status.' : 'Control Room data is unavailable.'}`
              : loading
                ? 'Loading current status.'
                : 'Control Room data is unavailable.'
        }
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <TimeRangePills value={period} options={CONTROL_ROOM_RANGES} onChange={setPeriod} />
            <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} loading={loading}>
              <RefreshCw className={loading ? 'animate-spin' : ''} />
              {loading ? 'Refreshing…' : 'Refresh'}
            </Button>
          </div>
        }
      />

      {error ? (
        <Panel
          padding="sm"
          tone="inset"
          toneAccent={overview ? 'warning' : 'critical'}
          title={overview ? 'Some dashboard data is unavailable' : 'Dashboard unavailable'}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className={overview ? 'text-sm text-text-secondary' : 'text-sm text-state-critical'} role="alert">
              {overview ? 'Showing the latest available data.' : error}
            </p>
            {!overview ? (
              <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
                Retry
              </Button>
            ) : null}
          </div>
        </Panel>
      ) : null}

      {stale ? (
        <p className="sr-only">Dashboard is showing the latest available data.</p>
      ) : null}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <ExecutiveMetricCard
          label={CONTROL_ROOM_COPY.infrastructureHealth}
          value={overview?.availability.estate ? `${overview.estate.groups_healthy} / ${overview.estate.groups_total}` : 'N/A'}
          hint={
            overview?.availability.estate
              ? `${overview.estate.nodes_healthy} / ${overview.estate.nodes_total} nodes healthy`
              : metricHint(loading)
          }
          tone={overview ? estateTone(overview) : 'unknown'}
          icon={<Server />}
          loading={loading && !overview}
          disabled={!overview?.availability.estate}
          onClick={() => { if (overview?.availability.estate) setDetail('infrastructure'); }}
        />
        <ExecutiveMetricCard
          label={CONTROL_ROOM_COPY.ruleViolations}
          value={overview?.availability.violations ? overview.violations.total : 'N/A'}
          hint={
            overview?.availability.violations
              ? compactMetricHint([
                  `${overview.violations.critical} critical`,
                  `${overview.violations.high} high`,
                  formatPeriodDelta(overview.violations.delta_pct, overview.violations.previous_total),
                ])
              : metricHint(loading)
          }
          tone={overview ? violationTone(overview) : 'unknown'}
          icon={<ListChecks />}
          loading={loading && !overview}
          disabled={!overview?.availability.violations}
          onClick={() => { if (overview?.availability.violations) setDetail('violations'); }}
        />
        <ExecutiveMetricCard
          label={CONTROL_ROOM_COPY.handledAutomatically}
          value={overview?.availability.response ? overview.response.handled_automatically : 'N/A'}
          hint={
            overview?.availability.response
              ? overview.response.handled_automatically > 0
                ? `${overview.response.blocked} blocked · ${overview.response.contained} contained · ${overview.response.remediated} remediated`
                : 'No verified automatic responses'
              : metricHint(loading)
          }
          tone={overview?.availability.response ? (overview.response.failed > 0 ? 'warning' : 'info') : 'unknown'}
          icon={<ShieldCheck />}
          loading={loading && !overview}
          disabled={!overview?.availability.response}
          onClick={() => { if (overview?.availability.response) setDetail('response'); }}
        />
        <ExecutiveMetricCard
          label={CONTROL_ROOM_COPY.needsAttention}
          value={overview?.availability.attention ? overview.attention.total : 'N/A'}
          hint={
            overview?.availability.attention
              ? `${overview.attention.critical} critical · ${overview.attention.approvals} approvals · ${overview.attention.reviews} reviews`
              : metricHint(loading)
          }
          tone={overview ? attentionTone(overview) : 'unknown'}
          icon={<ShieldAlert />}
          loading={loading && !overview}
          disabled={!overview?.availability.attention}
          onClick={() => { if (overview?.availability.attention) setDetail('attention'); }}
        />
      </div>

      {!overview && !loading ? (
        <EmptyState
          icon={<AlertTriangle />}
          title="Dashboard unavailable"
          description="Retry the Control Room."
          action={
            <Button type="button" variant="outline" onClick={() => void refresh()}>
              Retry
            </Button>
          }
        />
      ) : overview ? (
        <>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-12">
            <div className="xl:col-span-7">
              <AttentionQueue
                attention={overview.attention}
                available={overview.availability.attention}
              />
            </div>
            <div className="xl:col-span-5">
              <SecurityActivity overview={overview} />
            </div>
          </div>

          <ProtectionCoverage
            protection={overview.protection}
            available={overview.availability.protection}
            onOpen={() => setDetail('protection')}
          />

          <OverviewDetailSheet
            detail={detail}
            overview={overview}
            onClose={() => setDetail(null)}
          />
        </>
      ) : null}
    </div>
  );
}

function metricHint(loading: boolean): string {
  return loading ? 'Loading…' : CONTROL_ROOM_COPY.dataUnavailable;
}

function compactMetricHint(values: string[]): string {
  return values.filter(Boolean).join(' · ');
}

function estateTone(overview: NonNullable<ReturnType<typeof useExecutiveOverview>['overview']>): StateTone {
  if (!overview.availability.estate) return 'unknown';
  if (overview.estate.groups_critical > 0) return 'critical';
  if (overview.estate.groups_degraded > 0 || overview.estate.groups_unknown > 0) return 'warning';
  return 'healthy';
}

function violationTone(overview: NonNullable<ReturnType<typeof useExecutiveOverview>['overview']>): StateTone {
  if (!overview.availability.violations) return 'unknown';
  if (overview.violations.critical > 0 || overview.violations.high > 0) return 'warning';
  return overview.violations.total > 0 ? 'info' : 'healthy';
}

function attentionTone(overview: NonNullable<ReturnType<typeof useExecutiveOverview>['overview']>): StateTone {
  if (!overview.availability.attention) return 'unknown';
  if (overview.attention.critical > 0) return 'critical';
  return overview.attention.total > 0 ? 'warning' : 'healthy';
}
