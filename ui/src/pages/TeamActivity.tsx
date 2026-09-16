import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ClipboardList,
  Clock,
  Eye,
  MessageSquare,
  RefreshCw,
  ShieldCheck,
  UserX,
  Users,
  type LucideIcon,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  Alert,
  Chart,
  DataTable,
  EmptyState,
  KpiTile,
  Loader,
  Panel,
  Pagination,
  SectionHeader,
  StatusTag,
  TimeRangePills,
  type TimeRangeOption,
} from '../components/kit';
import { AssigneePicker } from '@/components/team/AssigneePicker';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { chartColors } from '@/lib/chartTheme';
import type {
  TeamActivityItem,
  TeamActivityKind,
  TeamAnalystMetric,
  TeamCoverageGaps,
  TeamMetricsResponse,
  TeamTrendPoint,
  PaginatedResponse,
  SOCCase,
  TeamUser,
} from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const TEAM_RANGES: TimeRangeOption[] = [
  { label: '1D', value: '1' },
  { label: '7D', value: '7' },
  { label: '30D', value: '30' },
  { label: '4M', value: '120' },
];

const FEED_LABEL: Record<TeamActivityKind, string> = {
  alert_reviewed: 'Reviewed alert',
  alert_resolved: 'Resolved alert',
  case_created: 'Created case',
  containment: 'Containment action',
  note_added: 'Added case note',
};

const FEED_ICON: Record<TeamActivityKind, LucideIcon> = {
  alert_reviewed: Eye,
  alert_resolved: CheckCircle2,
  case_created: ClipboardList,
  containment: ShieldCheck,
  note_added: MessageSquare,
};

function timeAgo(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  if (ms < 0 || ms < 60_000) return 'just now';
  const mins = Math.floor(ms / 60_000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '–';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const mins = Math.round(seconds / 60);
  if (mins < 60) return `${mins}m`;
  const hrs = Math.floor(mins / 60);
  return `${hrs}h ${mins % 60}m`;
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

interface TeamActivityState {
  metrics: TeamMetricsResponse | null;
  trends: TeamTrendPoint[];
  feed: TeamActivityItem[];
  gaps: TeamCoverageGaps | null;
}

function feedLink(item: TeamActivityItem): string | null {
  if (!item.link_id) return null;
  if (item.kind === 'case_created') return `/cases`;
  if (item.kind === 'alert_reviewed' || item.kind === 'alert_resolved') return `/alerts`;
  return null;
}

export function TeamActivity(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId, currentTenant, loading: tenantLoading } = useTenant();
  const [days, setDays] = useState('30');
  const [data, setData] = useState<TeamActivityState>({
    metrics: null,
    trends: [],
    feed: [],
    gaps: null,
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [openCases, setOpenCases] = useState<PaginatedResponse<SOCCase> | null>(null);
  const [teamUsers, setTeamUsers] = useState<TeamUser[]>([]);
  const [casePage, setCasePage] = useState(0);
  const casePageSize = 25;

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setLoading(tenantLoading);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const parsedDays = Number(days);
      const [metrics, trends, feed, gaps, cases, users] = await Promise.all([
        api.getTeamMetrics(currentTenantId, { days: parsedDays }),
        api.getTeamTrends(currentTenantId, { days: parsedDays }),
        api.getTeamActivity(currentTenantId, { days: parsedDays, limit: 50 }),
        api.getTeamCoverageGaps(currentTenantId),
        api.listSOCCases({
          tenantId: currentTenantId,
          status: 'open',
          limit: casePageSize,
          offset: casePage * casePageSize,
        }),
        api.getTeamUsers(currentTenantId),
      ]);
      setData({ metrics, trends: trends.points ?? [], feed: feed.data, gaps });
      setOpenCases(cases);
      setTeamUsers(users);
    } catch (err) {
      setError(errorMessage(err, 'Failed to load team activity.'));
    } finally {
      setLoading(false);
    }
  }, [api, currentTenantId, days, tenantLoading, casePage]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const summary = data.metrics?.summary ?? null;

  const trendChart = useMemo(() => {
    const colors = chartColors();
    const labels = data.trends.map((p) => new Date(p.bucket).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }));
    return {
      labels,
      datasets: [
        {
          label: 'Alerts reviewed',
          data: data.trends.map((p) => p.alerts_reviewed),
          borderColor: colors.brand,
          backgroundColor: colors.brand,
        },
        {
          label: 'Cases created',
          data: data.trends.map((p) => p.cases_created),
          borderColor: colors.accent,
          backgroundColor: colors.accent,
        },
        {
          label: 'Cases closed',
          data: data.trends.map((p) => p.cases_closed),
          borderColor: colors.healthy,
          backgroundColor: colors.healthy,
        },
      ],
    };
  }, [data.trends]);

  const containmentChart = useMemo(() => {
    const colors = chartColors();
    return {
      labels: data.trends.map((p) => new Date(p.bucket).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })),
      datasets: [
        {
          label: 'Containment actions',
          data: data.trends.map((p) => p.containment_actions),
          backgroundColor: colors.warning,
        },
      ],
    };
  }, [data.trends]);

  const analystColumns = useMemo<ColumnDef<TeamAnalystMetric, unknown>[]>(
    () => [
      {
        accessorKey: 'analyst_name',
        header: 'Analyst',
        cell: ({ row }) => (
          <span className="inline-flex items-center gap-2 font-medium text-foreground">
            <span className="flex h-6 w-6 items-center justify-center rounded-full bg-brand-500/10 text-[10px] font-semibold text-brand-400">
              {initials(row.original.analyst_name)}
            </span>
            {row.original.analyst_name || 'Unknown'}
          </span>
        ),
      },
      { accessorKey: 'alerts_reviewed', header: 'Alerts reviewed' },
      { accessorKey: 'alerts_resolved', header: 'Alerts resolved' },
      { accessorKey: 'cases_created', header: 'Cases created' },
      { accessorKey: 'containment_actions', header: 'Containment' },
      { accessorKey: 'notes_added', header: 'Notes' },
      {
        accessorKey: 'avg_time_to_investigate_seconds',
        header: 'Avg TTI',
        cell: ({ row }) => <span className="text-text-secondary">{formatDuration(row.original.avg_time_to_investigate_seconds)}</span>,
      },
    ],
    [],
  );

  const openCaseColumns = useMemo<ColumnDef<SOCCase, unknown>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => <StatusTag tone={row.original.severity === 'critical' ? 'critical' : row.original.severity === 'high' ? 'warning' : 'info'}>{row.original.severity}</StatusTag>,
    },
    {
      accessorKey: 'title',
      header: 'Case',
      cell: ({ row }) => <Link to="/cases" className="font-medium text-foreground hover:text-brand-400">{row.original.title}</Link>,
    },
    {
      accessorKey: 'created_at',
      header: 'Created',
      cell: ({ row }) => <span className="text-xs text-text-secondary">{timeAgo(row.original.created_at)}</span>,
    },
    {
      id: 'assignee',
      header: 'Owner',
      cell: ({ row }) => (
        <AssigneePicker
          value={row.original.assignee ?? null}
          options={teamUsers}
          onChange={(user) => {
            if (!currentTenantId) return;
            void api.assignSOCCase(row.original.case_id, currentTenantId, {
              assignee_id: user?.id ?? null,
            }).then((updated) => {
              setOpenCases((current) => current ? {
                ...current,
                data: current.data.map((item) => item.case_id === updated.case_id ? updated : item),
              } : current);
            }).catch((err) => setError(errorMessage(err, 'Unable to update assignee.')));
          }}
        />
      ),
    },
  ], [api, currentTenantId, teamUsers]);

  const gapCounts = data.gaps;
  const hasGaps = gapCounts
    ? gapCounts.unreviewed_open_alerts > 0 ||
      gapCounts.stale_cases > 0 ||
      gapCounts.inactive_analysts_90d > 0 ||
      gapCounts.open_alerts_older_than_24h > 0
    : false;

  const feedEmpty = !loading && data.feed.length === 0 && (data.metrics?.summary?.alerts_reviewed ?? 0) === 0;

  return (
    <div className="flex flex-col gap-6">
      <SectionHeader
        eyebrow="SOC OPS"
        title="Team activity"
        description={
          currentTenant?.name
            ? `${currentTenant.name}: what the team investigated over the selected window.`
            : 'What the team investigated over the selected window.'
        }
        actions={
          <div className="flex items-center gap-3">
            <TimeRangePills
              value={days}
              options={TEAM_RANGES}
              onChange={setDays}
              ariaLabel="Team activity window"
            />
            <Button size="sm" variant="outline" onClick={() => void refresh()} disabled={loading}>
              <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
              Refresh
            </Button>
          </div>
        }
      />

      {error && (
        <Alert
          variant="warning"
          title="Team activity unavailable"
          icon={AlertTriangle}
          role="alert"
          actions={
            <Button size="sm" variant="outline" onClick={() => void refresh()}>
              Retry
            </Button>
          }
        >
          {error}
        </Alert>
      )}

      {!currentTenantId && !tenantLoading && (
        <EmptyState title="Select a tenant" description="Pick a tenant from the tenant switcher to see team activity." />
      )}

      {currentTenantId && (
        <>
          {hasGaps && gapCounts && (
            <Alert
              variant="warning"
              title="Coverage gaps"
              icon={UserX}
              role="alert"
              actions={
                <div className="flex flex-wrap gap-2">
                  <StatusTag tone="warning">{`${gapCounts.unreviewed_open_alerts} unseen`}</StatusTag>
                  <StatusTag tone="warning">{`${gapCounts.open_alerts_older_than_24h} >24h`}</StatusTag>
                  <StatusTag tone="info">{`${gapCounts.stale_cases} stale cases`}</StatusTag>
                  <StatusTag tone="critical">{`${gapCounts.inactive_analysts_90d} inactive`}</StatusTag>
                </div>
              }
            >
              {[
                gapCounts.unreviewed_open_alerts > 0
                  ? `${gapCounts.unreviewed_open_alerts} open alert${gapCounts.unreviewed_open_alerts === 1 ? '' : 's'} never acknowledged`
                  : null,
                gapCounts.stale_cases > 0
                  ? `${gapCounts.stale_cases} case${gapCounts.stale_cases === 1 ? '' : 's'} without updates in 7 days`
                  : null,
                gapCounts.inactive_analysts_90d > 0
                  ? `${gapCounts.inactive_analysts_90d} analyst${gapCounts.inactive_analysts_90d === 1 ? '' : 's'} quiet for 7+ days`
                  : null,
              ]
                .filter(Boolean)
                .join(' · ') || 'Coverage looks healthy today.'}
            </Alert>
          )}

          {loading && !summary && <Loader label="Loading team activity…" />}

          {summary && (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
              <KpiTile icon={<Eye />} label="Alerts reviewed" value={summary.alerts_reviewed.toLocaleString()} />
              <KpiTile icon={<ClipboardList />} label="Cases created" value={summary.cases_created.toLocaleString()} />
              <KpiTile icon={<ShieldCheck />} label="Containment" value={summary.containment_actions.toLocaleString()} />
              <KpiTile icon={<Users />} label="Active analysts" value={summary.active_analysts.toLocaleString()} />
              <KpiTile icon={<Clock />} label="Avg TTI" value={formatDuration(summary.avg_time_to_investigate_seconds)} />
              <KpiTile icon={<Activity />} label="Avg TTR" value={formatDuration(summary.avg_time_to_resolve_seconds)} />
            </div>
          )}

          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            <Panel title="Team volume" eyebrow="Alert reviews, cases created and closed per period">
              {loading && !data.trends.length ? (
                <div className="flex flex-col gap-3">
                  <Skeleton className="h-40 w-full" />
                </div>
              ) : data.trends.length ? (
                <Chart kind="line" data={trendChart} height={240} ariaLabel="Trend of alerts reviewed, cases created and cases closed" />
              ) : (
                <EmptyState title="No trend data" description="Nothing happened in the selected window." />
              )}
            </Panel>
            <Panel title="Containment" eyebrow="Block / allow / quarantine actions per period">
              {loading && !data.trends.length ? (
                <Skeleton className="h-40 w-full" />
              ) : data.trends.length ? (
                <Chart kind="bar" data={containmentChart} height={240} ariaLabel="Containment actions per period" />
              ) : (
                <EmptyState title="No containment data" description="Nothing happened in the selected window." />
              )}
            </Panel>
          </div>

          <Panel title="Analyst workload" eyebrow="Work attributed to each analyst in the selected window">
            <DataTable
              columns={analystColumns}
              rows={data.metrics?.analysts ?? []}
              loading={loading && !data.metrics}
              rowKey={(row) => row.analyst_id}
              empty={
                <EmptyState
                  title="No analyst activity"
                  description="No attributed work in the selected window."
                />
              }
            />
          </Panel>

          <Panel title="Open cases" eyebrow="Cases awaiting action">
            <DataTable
              columns={openCaseColumns}
              rows={openCases?.data ?? []}
              rowKey={(row) => row.case_id}
              loading={loading}
              empty={<EmptyState title="No open cases" description="All cases are closed." />}
            />
            {openCases && openCases.pagination.total > casePageSize ? (
              <Pagination
                page={casePage}
                pageSize={casePageSize}
                total={openCases.pagination.total}
                onPageChange={setCasePage}
                className="mt-3"
              />
            ) : null}
          </Panel>

          <Panel title="Activity feed" eyebrow="Chronological record of analyst actions">
            {feedEmpty ? (
              <EmptyState
                title="No activity in this window"
                description="Narrow the range or check back after the team takes action."
              />
            ) : (
              <ol className="relative flex flex-col" aria-label="Team activity feed">
                {data.feed.map((item, index) => (
                  <FeedRow key={`${item.timestamp}:${index}`} item={item} />
                ))}
              </ol>
            )}
          </Panel>
        </>
      )}
    </div>
  );
}

function severityTone(severity: string) {
  switch (severity.toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'warning';
    default:
      return 'info';
  }
}

function FeedRow({ item }: { item: TeamActivityItem }): JSX.Element {
  const Icon = FEED_ICON[item.kind] ?? Activity;
  const link = feedLink(item);
  const tone =
    item.kind === 'alert_resolved'
      ? 'healthy'
      : item.kind === 'alert_reviewed'
        ? 'accent'
        : item.kind === 'containment'
          ? 'warning'
          : 'brand';
  return (
    <li className="relative flex gap-3 px-1 py-2.5">
      <span
        className={cn(
          'mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full',
          toneClass(tone),
        )}
      >
        <Icon className="h-3.5 w-3.5" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-sm">
          <span className="font-medium text-foreground">{item.actor_name || 'Unknown analyst'}</span>
          <span className="text-text-secondary">{timeAgo(item.timestamp)}</span>
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-xs text-text-secondary">{FEED_LABEL[item.kind]}</span>
          <span className="truncate text-sm text-text-secondary">{item.detail}</span>
          {item.severity && <StatusTag tone={severityTone(item.severity)}>{item.severity}</StatusTag>}
          {link && (
            <Link
              to={link}
              className="text-xs font-medium text-brand-400 hover:text-brand-300"
            >
              Open →
            </Link>
          )}
        </div>
      </div>
    </li>
  );
}

function toneClass(tone: string): string {
  switch (tone) {
    case 'healthy':
      return 'bg-state-healthy/10 text-state-healthy';
    case 'warning':
      return 'bg-state-warning/10 text-state-warning';
    case 'accent':
      return 'bg-accent-500/10 text-accent-400';
    default:
      return 'bg-brand-500/10 text-brand-400';
  }
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  const letters = (parts[0][0] ?? '') + (parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : '');
  return letters.toUpperCase();
}
