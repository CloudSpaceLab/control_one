import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Database,
  Globe2,
  LockKeyhole,
  RefreshCw,
  Server,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
  Wifi,
  WifiOff,
  Wrench,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import {
  EmptyState,
  KpiTile,
  Panel,
  PostureBar,
  SectionHeader,
  StatusTag,
  TimeRangePills,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { describeIPBehaviorFinding } from '../lib/ipBehaviorPresentation';
import type {
  ControlRoomAction,
  ControlRoomIncident,
  ControlRoomIPFinding,
  ControlRoomLane,
  ControlRoomOverview,
  ControlRoomIsolationNode,
  ControlRoomTone,
  ControlRoomWebserver,
  NetworkIsolationMode,
} from '../lib/api';

const CONTROL_ROOM_RANGES = [
  { label: '1H', value: '1h' },
  { label: '6H', value: '6h' },
  { label: '24H', value: '24h' },
  { label: '7D', value: '7d' },
  { label: '30D', value: '30d' },
];

const LANE_ICONS: Record<string, JSX.Element> = {
  'server-health': <Server />,
  security: <ShieldAlert />,
  'app-db-health': <Database />,
  exposure: <ShieldQuestion />,
  'ip-behavior': <Globe2 />,
  'patch-posture': <Wrench />,
};

const TONE_ACCENT: Record<ControlRoomTone, 'brand' | 'accent' | 'healthy' | 'warning' | 'critical'> = {
  healthy: 'healthy',
  warning: 'warning',
  degraded: 'warning',
  critical: 'critical',
  info: 'accent',
  unknown: 'brand',
};

export function ControlRoom(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId, currentTenant } = useTenant();
  const [period, setPeriod] = useState('24h');
  const [overview, setOverview] = useState<ControlRoomOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [webserverAction, setWebserverAction] = useState<Record<string, string>>({});
  const [isolationOpen, setIsolationOpen] = useState(false);
  const [isolationAction, setIsolationAction] = useState<Record<string, string>>({});

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const next = await api.getControlRoomOverview(currentTenantId, period);
      setOverview(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load the Control Room.');
    } finally {
      setLoading(false);
    }
  }, [api, currentTenantId, period]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const laneByID = useMemo(() => {
    const map = new Map<string, ControlRoomLane>();
    overview?.lanes.forEach((lane) => map.set(lane.id, lane));
    return map;
  }, [overview?.lanes]);

  const totalPending = useMemo(() => {
    return overview?.pending_actions.reduce((sum, action) => sum + action.count, 0) ?? 0;
  }, [overview?.pending_actions]);
  const headerDescription = overview
    ? `${currentTenant?.name ? `${currentTenant.name}: ` : ''}${overview.top_incidents.length} incidents, ${totalPending} pending actions, ${overview.ip_behavior.findings.length} IP findings in ${overview.period}.`
    : `${currentTenant?.name ? `${currentTenant.name}: ` : ''}Loading fleet status.`;

  const runWebserverAction = async (instance: ControlRoomWebserver, action: 'plan' | 'apply' | 'rollback') => {
    if (!currentTenantId) return;
    if (action !== 'plan') {
      const confirmed = window.confirm(
        action === 'apply'
          ? `Apply the managed Control One capture/enforcement policy to ${instance.kind} ${instance.service || instance.id}?`
          : `Queue rollback for ${instance.kind} ${instance.service || instance.id}?`,
      );
      if (!confirmed) return;
    }
    const key = `${instance.id}:${action}`;
    setWebserverAction((current) => ({ ...current, [key]: 'Queuing...' }));
    try {
      const payload = {
        tenant_id: currentTenantId,
        node_id: instance.node_id,
        policy: {
          mode: action === 'rollback' ? 'rollback' : 'capture',
          requested_from: 'control_room',
          approval_required: action !== 'plan',
        },
      };
      const response =
        action === 'plan'
          ? await api.planWebserverConfig(instance.id, payload)
          : action === 'apply'
            ? await api.applyWebserverConfig(instance.id, payload)
            : await api.rollbackWebserverConfig(instance.id, payload);
      setWebserverAction((current) => ({
        ...current,
        [key]: `${response.status || 'queued'} (${response.job_id.slice(0, 8)})`,
      }));
    } catch (err) {
      setWebserverAction((current) => ({
        ...current,
        [key]: err instanceof Error ? err.message : 'Action failed',
      }));
    }
  };

  const runIsolationAction = async (
    node: ControlRoomIsolationNode,
    mode: NetworkIsolationMode,
    durationSeconds?: number,
  ) => {
    const label = mode === 'online' ? 'return this node online' : `set ${node.hostname} to ${mode}`;
    if (!window.confirm(`Confirm ${label}?`)) return;
    const key = `${node.id}:${mode}`;
    setIsolationAction((current) => ({ ...current, [key]: 'Updating...' }));
    try {
      await api.setNodeIsolation(node.id, {
        mode,
        duration_seconds: mode === 'online' ? undefined : durationSeconds,
        reason: mode === 'online' ? 'Isolation cleared from Control Room' : 'Isolation set from Control Room',
        allowed_applications:
          mode === 'whitelist'
            ? node.allowed_applications?.length
              ? node.allowed_applications
              : ['control-one-agent', 'patch']
            : undefined,
        allowlist_cidrs: mode === 'whitelist' ? node.allowlist_cidrs : undefined,
      });
      setIsolationAction((current) => ({ ...current, [key]: 'Updated' }));
      await refresh();
    } catch (err) {
      setIsolationAction((current) => ({
        ...current,
        [key]: err instanceof Error ? err.message : 'Update failed',
      }));
    }
  };

  const quickQuestions = overview ? buildQuickQuestions(overview, laneByID, totalPending) : [];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="CONTROL ROOM"
        title="Fleet status"
        description={headerDescription}
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <TimeRangePills value={period} options={CONTROL_ROOM_RANGES} onChange={setPeriod} />
            <Button
              type="button"
              variant={isolationOpen ? 'secondary' : 'outline'}
              size="icon"
              aria-label="Network isolation"
              title="Network isolation"
              onClick={() => setIsolationOpen((open) => !open)}
            >
              {overview && overview.isolation.airgapped > 0 ? <WifiOff /> : overview && overview.isolation.whitelist > 0 ? <LockKeyhole /> : <Wifi />}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} loading={loading}>
              <RefreshCw />
              Refresh
            </Button>
          </div>
        }
      />

      {error ? (
        <Panel toneAccent="critical" title="Overview data unavailable">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      ) : null}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-6">
        {loading && !overview
          ? Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-28 rounded-lg" />)
          : quickQuestions.map((question) => (
              <KpiTile
                key={question.label}
                size="sm"
                label={question.label}
                value={question.value}
                tone={normalizeTone(question.tone)}
                hint={question.hint}
                icon={question.icon}
              />
        ))}
      </div>

      {overview && isolationOpen ? (
        <IsolationPosturePanel
          overview={overview}
          actionState={isolationAction}
          onAction={(node, mode, durationSeconds) => void runIsolationAction(node, mode, durationSeconds)}
        />
      ) : null}

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-12">
        <ExposureConfidencePanel
          lane={laneByID.get('exposure')}
          loading={loading && !overview}
          className="xl:col-span-5"
        />
        <SignalMapPanel
          lanes={overview?.lanes ?? []}
          loading={loading && !overview}
          className="xl:col-span-4"
        />
        <ActionQueuePanel
          overview={overview}
          totalPending={totalPending}
          loading={loading && !overview}
          className="xl:col-span-3"
        />
      </div>

      <div className="grid grid-cols-1 gap-4">
        <Panel
          eyebrow="UNUSUAL NOW"
          title="Connection/IP behavior"
          toneAccent={TONE_ACCENT[overview?.lanes.find((lane) => lane.id === 'ip-behavior')?.tone ?? 'unknown']}
          actions={
            <Button asChild variant="ghost" size="sm">
              <Link to="/security/network?tab=ip-behavior">
                Drill down
                <ArrowRight />
              </Link>
            </Button>
          }
        >
          {loading && !overview ? (
            <Skeleton className="h-48 rounded-lg" />
          ) : overview && overview.ip_behavior.findings.length > 0 ? (
            <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
              {overview.ip_behavior.findings.slice(0, 6).map((finding) => (
                <IPBehaviorFindingCard key={finding.id} finding={finding} />
              ))}
            </div>
          ) : (
            <EmptyState
              tone="success"
              icon={<ShieldCheck />}
              title="No open IP behavior anomalies"
              description="No backend IP behavior findings in this window."
            />
          )}

          {overview && overview.ip_behavior.countries.length > 0 ? (
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
              {overview.ip_behavior.countries.slice(0, 3).map((country) => (
                <Link
                  key={country.country_code}
                  to={`/security/network?tab=ip-behavior&country=${encodeURIComponent(country.country_code)}`}
                  className="rounded-lg border border-border-subtle bg-surface p-3 transition hover:border-border-strong hover:bg-hover"
                >
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-medium text-foreground">{country.country || country.country_code}</p>
                      <p className="text-xs text-text-muted">{country.unique_source_ips} unique IPs</p>
                    </div>
                    <Globe2 className="h-4 w-4 text-text-muted" />
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
                    <MetricText label="Requests" value={formatNumber(country.request_count)} />
                    <MetricText label="Bytes out" value={formatBytes(country.bytes_out)} />
                    <MetricText label="401/403" value={formatNumber((country.status_counts?.['401'] ?? 0) + (country.status_counts?.['403'] ?? 0))} />
                    <MetricText label="5xx" value={formatNumber((country.status_counts?.['500'] ?? 0) + (country.status_counts?.['502'] ?? 0) + (country.status_counts?.['503'] ?? 0))} />
                  </div>
                  <p className="mt-3 truncate text-xs text-text-muted">
                    {compactList([...(country.top_asns ?? []), ...(country.top_apps ?? []), ...(country.server_groups ?? [])]) || 'No ASN/app/group labels'}
                  </p>
                </Link>
              ))}
            </div>
          ) : null}
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <Panel
          className="xl:col-span-2"
          eyebrow="INCIDENTS"
          title="Top incidents"
          toneAccent="warning"
          actions={
            <Button asChild variant="ghost" size="sm">
              <Link to="/cases">
                Open cases
                <ArrowRight />
              </Link>
            </Button>
          }
        >
          {loading && !overview ? (
            <Skeleton className="h-40 rounded-lg" />
          ) : overview && overview.top_incidents.length > 0 ? (
            <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
              {overview.top_incidents.slice(0, 6).map((incident) => (
                <IncidentRow key={`${incident.source}:${incident.id}`} incident={incident} />
              ))}
            </div>
          ) : (
            <EmptyState tone="success" icon={<ShieldCheck />} title="No incidents in this window" />
          )}
        </Panel>

        <Panel
          eyebrow="WEBSERVER AUTO-CONTROL"
          title="Capture and enforcement"
          toneAccent="brand"
          actions={
            <Button asChild variant="ghost" size="sm">
              <Link to="/security/webservers">
                Open details
                <ArrowRight className="ml-1 h-3.5 w-3.5" />
              </Link>
            </Button>
          }
        >
          {loading && !overview ? (
            <Skeleton className="h-48 rounded-lg" />
          ) : overview && overview.webservers.instances.length > 0 ? (
            <div className="flex flex-col gap-3">
              <div className="grid grid-cols-3 gap-2 text-xs">
                <MetricText label="Detected" value={formatNumber(overview.webservers.total)} />
                <MetricText label="Capture ready" value={formatNumber(overview.webservers.capture_ready)} />
                <MetricText label="Enforce ready" value={formatNumber(overview.webservers.enforce_ready)} />
              </div>
              <div className="flex max-h-80 flex-col gap-2 overflow-y-auto pr-1">
                {overview.webservers.instances.slice(0, 6).map((instance) => (
                  <div key={instance.id} className="rounded-lg border border-border-subtle bg-surface p-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-foreground">{instance.kind} {instance.service || 'default'}</p>
                        <p className="truncate font-mono text-xs text-text-muted">{instance.config_path || instance.log_path || instance.node_id}</p>
                      </div>
                      <StatusTag tone={instance.capture_ready && instance.enforce_ready ? 'healthy' : 'warning'}>
                        {instance.capture_ready && instance.enforce_ready ? 'ready' : 'gap'}
                      </StatusTag>
                    </div>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button type="button" variant="outline" size="sm" onClick={() => void runWebserverAction(instance, 'plan')}>
                        Plan
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={() => void runWebserverAction(instance, 'apply')}>
                        Apply
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={() => void runWebserverAction(instance, 'rollback')}>
                        Rollback
                      </Button>
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
                      <MetricText label="Vhosts" value={formatNumber(instance.vhosts?.length ?? 0)} />
                      <MetricText label="Observed" value={formatShortDate(instance.observed_at)} />
                    </div>
                    {instance.last_action ? (
                      <div className="mt-3 rounded-md border border-border-subtle bg-elevated px-2.5 py-2 text-xs">
                        <div className="flex items-center justify-between gap-2">
                          <span className="truncate text-text-secondary">{webserverActionLabel(instance.last_action.action)}</span>
                          <StatusTag tone={webserverStatusTone(instance.last_action.status)}>{instance.last_action.status}</StatusTag>
                        </div>
                        {instance.last_action.error_message ? (
                          <p className="mt-1 truncate text-state-critical">{instance.last_action.error_message}</p>
                        ) : null}
                      </div>
                    ) : null}
                    {instance.last_receipt ? (
                      <div className="mt-2 rounded-md border border-border-subtle bg-elevated px-2.5 py-2 text-xs text-text-secondary">
                        <div className="flex items-center justify-between gap-2">
                          <span>validation {instance.last_receipt.validation_status || 'unknown'}</span>
                          <span>reload {instance.last_receipt.reload_status || 'unknown'}</span>
                        </div>
                        {instance.last_receipt.rollback_ref ? (
                          <p className="mt-1 truncate">rollback {instance.last_receipt.rollback_ref}</p>
                        ) : null}
                      </div>
                    ) : null}
                    {['plan', 'apply', 'rollback'].map((action) => {
                      const status = webserverAction[`${instance.id}:${action}`];
                      return status ? (
                        <p key={action} className="mt-2 text-xs text-text-muted">
                          {action}: {status}
                        </p>
                      ) : null;
                    })}
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <EmptyState
              icon={<Server />}
              title="No webserver inventory yet"
              description="No nginx, Apache, lighttpd, Tomcat, or edge proxy instances reported."
            />
          )}
        </Panel>
      </div>
    </div>
  );
}

function IPBehaviorFindingCard({ finding }: { finding: ControlRoomIPFinding }) {
  const presentation = describeIPBehaviorFinding(finding, { countryLabel: finding.country_code, maxSignals: 3 });
  const confidenceTone = severityToTone(finding.severity);
  return (
    <Link
      to={finding.drilldown}
      className="block rounded-lg border border-border-subtle bg-surface p-3 transition hover:border-border-strong hover:bg-hover"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-foreground">{presentation.categoryLabel}</p>
          <p className="truncate font-mono text-xs text-text-muted">{presentation.source}</p>
        </div>
        <StatusTag tone={confidenceTone}>{presentation.confidence}%</StatusTag>
      </div>
      <p className="mt-2 line-clamp-2 text-xs leading-5 text-text-secondary">{presentation.summary}</p>
      <div className="mt-3 flex flex-wrap gap-1.5">
        {presentation.alertLabel && (
          <StatusTag tone={presentation.confidence >= 100 ? 'critical' : 'warning'} icon={presentation.confidence >= 100 ? <AlertTriangle className="h-3 w-3" /> : undefined}>
            {presentation.alertLabel}
          </StatusTag>
        )}
        {presentation.signals.map((signal) => (
          <span key={signal} className="rounded border border-border-subtle bg-elevated px-2 py-0.5 text-[11px] text-text-secondary">
            {signal}
          </span>
        ))}
        {presentation.hiddenSignalCount > 0 && (
          <span className="rounded border border-border-subtle bg-elevated px-2 py-0.5 text-[11px] text-text-muted">
            +{presentation.hiddenSignalCount} more
          </span>
        )}
      </div>
      <div className="mt-3 text-[11px] text-text-muted">Last seen {formatDateTime(finding.last_seen_at)}</div>
    </Link>
  );
}

function ExposureConfidencePanel({ lane, loading, className }: { lane?: ControlRoomLane; loading: boolean; className?: string }) {
  const confidence = lane?.score ?? 0;
  const publicListeners = lane ? metricByLabel(lane, 'Public listeners') : undefined;
  const protectedListeners = lane ? metricByLabel(lane, 'Protected listeners') : undefined;
  const criticalGaps = lane ? metricByLabel(lane, 'Critical gaps') : undefined;
  const firewallGaps = lane ? metricByLabel(lane, 'Public firewall gaps') : undefined;
  const webReady = lane ? metricByLabel(lane, 'Web block ready') : undefined;

  return (
    <Panel
      className={className}
      eyebrow="NETWORK EXPOSURE"
      title="Security confidence"
      toneAccent={TONE_ACCENT[lane?.tone ?? 'unknown']}
      actions={
        <Button asChild variant="ghost" size="sm">
          <Link to="/control-room/exposure">
            Details
            <ArrowRight />
          </Link>
        </Button>
      }
    >
      {loading ? (
        <Skeleton className="h-72 rounded-lg" />
      ) : lane ? (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-[11rem_1fr]">
          <div className="flex items-center justify-center">
            <ConfidenceDial score={confidence} tone={normalizeTone(lane.tone)} />
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <StatusTag tone={normalizeTone(lane.tone)}>{lane.primary_metric.hint || exposureConfidenceLabel(confidence)}</StatusTag>
                <span className="font-mono text-xs text-text-muted">{lane.primary_metric.value}</span>
              </div>
              <p className="mt-2 text-sm text-text-secondary">{lane.summary}</p>
            </div>
            <PostureBar score={confidence} ariaLabel={`Network exposure security confidence ${confidence}%`} showLabels />
            <div className="grid grid-cols-2 gap-2">
              <MetricText label={publicListeners?.label ?? 'Public listeners'} value={publicListeners?.value ?? '0'} />
              <MetricText label={protectedListeners?.label ?? 'Protected'} value={protectedListeners?.value ?? '0'} />
              <MetricText label={criticalGaps?.label ?? 'Critical gaps'} value={criticalGaps?.value ?? '0'} />
              <MetricText label={firewallGaps?.label ?? 'Firewall gaps'} value={firewallGaps?.value ?? '0'} />
            </div>
            {webReady ? (
              <div className="rounded-md border border-border-subtle bg-surface px-3 py-2 text-xs text-text-secondary">
                Web enforcement readiness: <span className="font-mono text-foreground">{webReady.value}</span>
              </div>
            ) : null}
            {lane.items?.length ? (
              <div className="flex flex-col gap-2">
                {lane.items.slice(0, 3).map((item) => (
                  <Link
                    key={`${item.label}:${item.value}`}
                    to={item.drilldown || lane.drilldown}
                    className="flex items-center justify-between gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2 text-xs hover:bg-hover"
                  >
                    <span className="min-w-0 truncate text-text-secondary">{item.hint || item.label}</span>
                    <span className={cn('font-mono font-semibold tabular-nums', toneText(item.tone))}>{item.value}</span>
                  </Link>
                ))}
              </div>
            ) : null}
          </div>
        </div>
      ) : (
        <EmptyState icon={<ShieldQuestion />} title="No exposure score" description="Exposure confidence appears after inventory reports listeners and firewall state." />
      )}
    </Panel>
  );
}

function SignalMapPanel({ lanes, loading, className }: { lanes: ControlRoomLane[]; loading: boolean; className?: string }) {
  const groups = [
    { label: 'Protect', ids: ['exposure', 'security', 'ip-behavior'] },
    { label: 'Operate', ids: ['server-health', 'app-db-health', 'patch-posture'] },
  ];

  return (
    <Panel className={className} eyebrow="SIGNAL MAP" title="Organized control lanes" toneAccent="accent">
      {loading ? (
        <Skeleton className="h-72 rounded-lg" />
      ) : lanes.length > 0 ? (
        <div className="flex flex-col gap-4">
          {groups.map((group) => {
            const rows = group.ids
              .map((id) => lanes.find((lane) => lane.id === id))
              .filter((lane): lane is ControlRoomLane => Boolean(lane));
            if (rows.length === 0) return null;
            return (
              <div key={group.label} className="flex flex-col gap-2">
                <p className="text-xs font-semibold uppercase tracking-wide text-text-secondary">{group.label}</p>
                {rows.map((lane) => (
                  <SignalLaneRow key={lane.id} lane={lane} />
                ))}
              </div>
            );
          })}
        </div>
      ) : (
        <EmptyState title="No lanes yet" description="Control lanes appear after the overview endpoint responds." />
      )}
    </Panel>
  );
}

function SignalLaneRow({ lane }: { lane: ControlRoomLane }) {
  return (
    <Link
      to={controlRoomLaneDetailPath(lane.id)}
      className="grid grid-cols-[auto_1fr_auto] items-center gap-3 rounded-lg border border-border-subtle bg-surface px-3 py-2.5 transition hover:border-border-strong hover:bg-hover"
    >
      <span className={cn('rounded-md border border-border-subtle bg-elevated p-2 text-text-muted [&_svg]:h-4 [&_svg]:w-4', toneText(lane.tone))}>
        {LANE_ICONS[lane.id] ?? <Activity />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium text-foreground">{lane.title}</span>
        <span className="block truncate text-xs text-text-muted">{lane.primary_metric.label}: {lane.primary_metric.value}</span>
        <PostureBar score={lane.score} ariaLabel={`${lane.title} score ${lane.score}`} className="mt-2" />
      </span>
      <StatusTag tone={normalizeTone(lane.tone)}>{lane.score}</StatusTag>
    </Link>
  );
}

function ActionQueuePanel({
  overview,
  totalPending,
  loading,
  className,
}: {
  overview: ControlRoomOverview | null;
  totalPending: number;
  loading: boolean;
  className?: string;
}) {
  return (
    <Panel className={className} eyebrow="ACTION QUEUE" title="Operator decisions" toneAccent={totalPending > 0 ? 'warning' : 'healthy'}>
      {loading ? (
        <Skeleton className="h-72 rounded-lg" />
      ) : overview && overview.pending_actions.some((action) => action.count > 0) ? (
        <div className="flex flex-col gap-2">
          {overview.pending_actions.map((action) => (
            <ActionRow key={action.id} action={action} />
          ))}
        </div>
      ) : (
        <EmptyState
          tone="success"
          icon={<ShieldCheck />}
          title="No approvals waiting"
          description="Patch approvals, block proposals, and enforcement queues are clear."
        />
      )}
      {overview?.stale_warnings.length ? (
        <div className="rounded-lg border border-border-subtle bg-surface p-3">
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-secondary">Stale data</p>
          <div className="flex flex-col gap-2">
            {overview.stale_warnings.slice(0, 3).map((warning) => (
              <Link key={warning.id} to={warning.drilldown || '/nodes'} className="text-xs text-text-secondary hover:text-foreground">
                {warning.message}
              </Link>
            ))}
          </div>
        </div>
      ) : null}
    </Panel>
  );
}

function ConfidenceDial({ score, tone }: { score: number; tone: StateTone }) {
  const clamped = Math.max(0, Math.min(100, score));
  return (
    <div
      className="relative grid h-40 w-40 shrink-0 place-items-center rounded-full border border-border-subtle"
      style={{
        background: `conic-gradient(${toneColor(tone)} ${clamped * 3.6}deg, var(--bg-surface-2) 0deg)`,
      }}
      role="img"
      aria-label={`Security confidence ${clamped}%`}
    >
      <div className="grid h-28 w-28 place-items-center rounded-full border border-border-subtle bg-elevated text-center shadow-[var(--shadow-panel)]">
        <div>
          <div className="font-mono text-3xl font-semibold tabular-nums text-foreground">{clamped}%</div>
          <div className="mt-1 text-xs text-text-muted">{exposureConfidenceLabel(clamped)}</div>
        </div>
      </div>
    </div>
  );
}

function metricByLabel(lane: ControlRoomLane, label: string) {
  if (lane.primary_metric.label === label) return lane.primary_metric;
  if (lane.secondary_metric.label === label) return lane.secondary_metric;
  return lane.metrics.find((metric) => metric.label === label);
}

function exposureConfidenceLabel(score: number): string {
  if (score < 50) return 'urgent';
  if (score < 75) return 'needs work';
  if (score < 90) return 'steady';
  return 'strong';
}

function toneColor(tone: StateTone): string {
  switch (tone) {
    case 'healthy':
      return 'var(--state-healthy)';
    case 'warning':
      return 'var(--state-warning)';
    case 'degraded':
      return 'var(--state-degraded)';
    case 'critical':
      return 'var(--state-critical)';
    case 'info':
      return 'var(--state-info)';
    default:
      return 'var(--state-unknown)';
  }
}

function ActionRow({ action }: { action: ControlRoomAction }) {
  return (
    <Link
      to={action.drilldown}
      className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-surface p-3 transition hover:border-border-strong hover:bg-hover"
    >
      <div>
        <p className="text-sm font-medium text-foreground">{action.label}</p>
        <p className="text-xs text-text-muted">Open queue</p>
      </div>
      <StatusTag tone={normalizeTone(action.tone)}>{action.count}</StatusTag>
    </Link>
  );
}

function IncidentRow({ incident }: { incident: ControlRoomIncident }) {
  const body = (
    <div className="flex items-start justify-between gap-4 p-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-foreground">{incident.title}</p>
        <p className="mt-1 text-xs text-text-secondary">{incident.summary || incident.source}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <StatusTag tone={severityToTone(incident.severity)}>{incident.severity}</StatusTag>
        <span className="hidden text-xs text-text-muted sm:inline">{formatDateTime(incident.opened_at)}</span>
      </div>
    </div>
  );
  if (!incident.drilldown) return body;
  return (
    <Link to={incident.drilldown} className="block hover:bg-hover">
      {body}
    </Link>
  );
}

function MetricText({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-elevated px-2.5 py-2">
      <p className="text-[0.68rem] uppercase tracking-wide text-text-muted">{label}</p>
      <p className="mt-0.5 font-mono text-sm font-semibold tabular-nums text-foreground">{value}</p>
    </div>
  );
}

function IsolationPosturePanel({
  overview,
  actionState,
  onAction,
}: {
  overview: ControlRoomOverview;
  actionState: Record<string, string>;
  onAction: (node: ControlRoomIsolationNode, mode: NetworkIsolationMode, durationSeconds?: number) => void;
}) {
  const nodes = overview.isolation.nodes.slice(0, 10);
  return (
    <Panel
      eyebrow="NETWORK ISOLATION"
      title="Firewall posture"
      toneAccent={overview.isolation.expired > 0 ? 'warning' : overview.isolation.airgapped + overview.isolation.whitelist > 0 ? 'healthy' : 'brand'}
      actions={
        <Button asChild variant="ghost" size="sm">
          <Link to="/control-room/exposure">
            Exposure details
            <ArrowRight />
          </Link>
        </Button>
      }
    >
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4 xl:grid-cols-7">
        <MetricText label="Online" value={formatNumber(overview.isolation.online)} />
        <MetricText label="Protected" value={formatNumber(overview.isolation.protected)} />
        <MetricText label="Whitelist-only" value={formatNumber(overview.isolation.whitelist)} />
        <MetricText label="Whitelist gaps" value={formatNumber(overview.isolation.whitelist_gaps)} />
        <MetricText label="Airgapped" value={formatNumber(overview.isolation.airgapped)} />
        <MetricText label="Expiring" value={formatNumber(overview.isolation.expiring_soon)} />
        <MetricText label="Expired" value={formatNumber(overview.isolation.expired)} />
      </div>

      {nodes.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border border-border-subtle">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
              <tr>
                <th className="px-3 py-2">Node</th>
                <th className="px-3 py-2">Mode</th>
                <th className="px-3 py-2">Allowed path</th>
                <th className="px-3 py-2">Timer</th>
                <th className="px-3 py-2 text-right">Action</th>
              </tr>
            </thead>
            <tbody>
              {nodes.map((node) => (
                <tr key={node.id} className="border-t border-border-subtle">
                  <td className="px-3 py-2">
                    <Link to={`/nodes/${node.id}`} className="font-medium text-foreground hover:underline">
                      {node.hostname}
                    </Link>
                    {node.reason ? <p className="mt-0.5 max-w-[18rem] truncate text-xs text-text-muted">{node.reason}</p> : null}
                  </td>
                  <td className="px-3 py-2">
                    <StatusTag tone={isolationTone(node)}>{isolationLabel(node.mode, node.expired)}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">
                    {node.local_only
                      ? 'local connectivity only'
                      : compactList([...(node.allowed_applications ?? []), ...(node.allowlist_cidrs ?? [])]) || (node.mode === 'online' ? 'online' : 'missing allowlist labels')}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-muted">{node.expires_at ? formatDateTime(node.expires_at) : 'no timer'}</td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap justify-end gap-2">
                      <Button type="button" variant="outline" size="sm" onClick={() => onAction(node, 'airgapped', 60 * 60)} disabled={node.active && node.mode === 'airgapped'}>
                        <WifiOff />
                        1h
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={() => onAction(node, 'whitelist', 24 * 60 * 60)} disabled={node.active && node.mode === 'whitelist' && isolationNodeHasCoverage(node)}>
                        <LockKeyhole />
                        24h
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={() => onAction(node, 'online')} disabled={!node.active && !node.expired}>
                        <Wifi />
                        Online
                      </Button>
                    </div>
                    {(['airgapped', 'whitelist', 'online'] as const).map((mode) => {
                      const status = actionState[`${node.id}:${mode}`];
                      return status ? (
                        <p key={mode} className="mt-1 text-right text-xs text-text-muted">
                          {status}
                        </p>
                      ) : null;
                    })}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState
          icon={<ShieldCheck />}
          title="No nodes in isolation scope"
          description="Node isolation controls appear after inventory reports nodes."
        />
      )}
      <p className="text-xs text-text-muted">
        Airgapped nodes are treated as intentionally offline. Whitelist-only nodes count as protected only when allowed applications or CIDRs are present.
      </p>
    </Panel>
  );
}

function buildQuickQuestions(
  overview: ControlRoomOverview,
  lanes: Map<string, ControlRoomLane>,
  pending: number,
) {
  const server = lanes.get('server-health');
  const exposure = lanes.get('exposure');
  const ip = lanes.get('ip-behavior');
  const patch = lanes.get('patch-posture');
  const security = lanes.get('security');
  const app = lanes.get('app-db-health');
  const topIPFinding = overview.ip_behavior.findings[0];
  const topIPFindingPresentation = topIPFinding
    ? describeIPBehaviorFinding(topIPFinding, { countryLabel: topIPFinding.country_code, maxSignals: 3 })
    : null;
  return [
    {
      label: 'Health incidents',
      value: server?.secondary_metric.value ?? '0',
      tone: server?.secondary_metric.tone ?? 'unknown',
      hint: server?.summary ?? 'No server status',
      icon: <AlertTriangle />,
    },
    {
      label: 'Exposure',
      value: exposure?.primary_metric.value ?? '0',
      tone: exposure?.primary_metric.tone ?? 'unknown',
      hint: exposure?.summary ?? 'No exposure data',
      icon: <ShieldQuestion />,
    },
    {
      label: 'Attack signals',
      value: ip?.primary_metric.value ?? '0',
      tone: ip?.tone ?? 'unknown',
      hint: topIPFindingPresentation?.summary || ip?.summary || 'No IP findings',
      icon: <Globe2 />,
    },
    {
      label: 'Patch failures',
      value: patch?.secondary_metric.value ?? '0',
      tone: patch?.secondary_metric.tone ?? 'unknown',
      hint: patch?.summary ?? 'No patch data',
      icon: <Wrench />,
    },
    {
      label: 'Approvals',
      value: String(pending),
      tone: pending > 0 ? 'warning' : 'healthy',
      hint: pending > 0 ? 'Review required' : 'No approvals waiting',
      icon: <ShieldAlert />,
    },
    {
      label: 'Automation',
      value: overview.pending_actions.find((a) => a.id === 'block-enforcement')?.count.toString() ?? '0',
      tone: security?.tone || app?.tone || 'info',
      hint: 'Block enforcement pending',
      icon: <Activity />,
    },
  ];
}

function normalizeTone(tone?: string): StateTone {
  switch (tone) {
    case 'healthy':
    case 'warning':
    case 'degraded':
    case 'critical':
    case 'info':
    case 'unknown':
      return tone;
    default:
      return 'unknown';
  }
}

function severityToTone(severity?: string): StateTone {
  switch ((severity || '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'warning';
    case 'medium':
    case 'low':
    case 'watch':
      return 'info';
    default:
      return 'unknown';
  }
}

function isolationTone(node: ControlRoomIsolationNode): StateTone {
  if (node.expired) return 'warning';
  switch ((node.mode || '').toLowerCase()) {
    case 'airgapped':
      return 'healthy';
    case 'whitelist':
      return isolationNodeHasCoverage(node) ? 'healthy' : 'warning';
    case 'online':
      return 'info';
    default:
      return 'unknown';
  }
}

function isolationLabel(mode?: string, expired?: boolean): string {
  if (expired) return 'expired';
  switch ((mode || '').toLowerCase()) {
    case 'airgapped':
      return 'airgapped';
    case 'whitelist':
      return 'whitelist-only';
    case 'online':
      return 'online';
    default:
      return mode || 'unknown';
  }
}

function isolationNodeHasCoverage(node: ControlRoomIsolationNode): boolean {
  return Boolean(node.local_only || node.allowed_applications?.length || node.allowlist_cidrs?.length);
}

function toneText(tone?: string): string {
  switch (normalizeTone(tone)) {
    case 'critical':
      return 'text-state-critical';
    case 'warning':
    case 'degraded':
      return 'text-state-warning';
    case 'healthy':
      return 'text-state-healthy';
    case 'info':
      return 'text-state-info';
    default:
      return 'text-text-muted';
  }
}

function formatNumber(value?: number): string {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value ?? 0);
}

function formatBytes(value?: number): string {
  const bytes = value ?? 0;
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let current = bytes / 1024;
  let idx = 0;
  while (current >= 1024 && idx < units.length - 1) {
    current /= 1024;
    idx++;
  }
  return `${current.toFixed(current >= 10 ? 0 : 1)} ${units[idx]}`;
}

function formatDateTime(value?: string): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function formatShortDate(value?: string): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'unknown';
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

function compactList(values?: Array<string | undefined | null>): string {
  return (values ?? [])
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value))
    .slice(0, 4)
    .join(', ');
}

function webserverStatusTone(status?: string): StateTone {
  switch ((status || '').toLowerCase()) {
    case 'succeeded':
    case 'success':
    case 'completed':
    case 'active':
      return 'healthy';
    case 'failed':
    case 'error':
      return 'critical';
    case 'pending':
    case 'running':
    case 'queued':
      return 'warning';
    default:
      return 'unknown';
  }
}

function webserverActionLabel(action?: string): string {
  return (action || 'webserver action')
    .replace(/^webserver\./, '')
    .replace(/_/g, ' ');
}

function controlRoomLaneDetailPath(laneId: string, metric?: string): string {
  const search = new URLSearchParams();
  if (metric) search.set('metric', metric);
  const qs = search.toString();
  return `/control-room/${encodeURIComponent(laneId)}${qs ? `?${qs}` : ''}`;
}

export default ControlRoom;
