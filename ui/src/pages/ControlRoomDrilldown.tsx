import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, ArrowRight, ClipboardList, FileText, History, ListChecks, LockKeyhole, RefreshCw, ShieldCheck, WifiOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  EmptyState,
  KpiTile,
  Panel,
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
  ControlRoomFirewallNode,
  ControlRoomIncident,
  ControlRoomIsolationNode,
  ControlRoomLane,
  ControlRoomOverview,
  ControlRoomPublicListener,
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

const SOURCE_ROUTES: Record<string, string> = {
  'server-health': '/nodes',
  security: '/alerts',
  'app-db-health': '/nodes',
  exposure: '/security/network',
  'ip-behavior': '/security/network?tab=ip-behavior',
  'patch-posture': '/infrastructure/patch',
};

const LANE_SOURCES: Record<string, string[]> = {
  'server-health': ['server_health', 'health'],
  security: ['alert', 'security'],
  'ip-behavior': ['ip_behavior'],
  'patch-posture': ['patch'],
};

const TONE_ACCENT: Record<ControlRoomTone, 'brand' | 'accent' | 'healthy' | 'warning' | 'critical'> = {
  healthy: 'healthy',
  warning: 'warning',
  degraded: 'warning',
  critical: 'critical',
  info: 'accent',
  unknown: 'brand',
};

export function ControlRoomDrilldown(): JSX.Element {
  const { laneId = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const api = useApiClient();
  const { currentTenantId, currentTenant, loading: tenantLoading } = useTenant();
  const [period, setPeriod] = useState(params.get('period') || '24h');
  const [overview, setOverview] = useState<ControlRoomOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setOverview(null);
      setError(null);
      setLoading(tenantLoading);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const next = await api.getControlRoomOverview(currentTenantId, period);
      setOverview(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Control Room data unavailable');
    } finally {
      setLoading(false);
    }
  }, [api, currentTenantId, period, tenantLoading]);

  useEffect(() => {
    const next = new URLSearchParams(params);
    next.set('period', period);
    setParams(next, { replace: true });
    void refresh();
    // params/setParams are intentionally excluded: this effect owns period sync.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period, refresh]);

  const lane = useMemo(() => overview?.lanes.find((row) => row.id === laneId), [laneId, overview?.lanes]);
  const selectedMetric = params.get('metric') || lane?.primary_metric.label || '';
  const sourceRoute = lane ? SOURCE_ROUTES[lane.id] || lane.drilldown : '/control-room';

  if (!loading && overview && !lane) {
    return <Navigate to="/control-room" replace />;
  }

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="CONTROL ROOM DETAIL"
        title={lane?.title || 'Lane detail'}
        description={lane ? detailDescription(currentTenant?.name, overview, lane) : 'Loading lane detail.'}
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <TimeRangePills value={period} options={CONTROL_ROOM_RANGES} onChange={setPeriod} />
            <Button type="button" variant="outline" size="sm" onClick={() => void refresh()} loading={loading}>
              <RefreshCw />
              Refresh
            </Button>
            <Button asChild variant="ghost" size="sm">
              <Link to="/control-room">
                <ArrowLeft />
                Control Room
              </Link>
            </Button>
          </div>
        }
      />

      {error ? (
        <Panel toneAccent="critical" title="Detail data unavailable">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      ) : null}

      {loading && !lane ? (
        <Skeleton className="h-96 rounded-lg" />
      ) : lane && overview ? (
        <>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
            {[lane.primary_metric, lane.secondary_metric, ...lane.metrics].slice(0, 4).map((metric) => (
              <KpiTile
                key={metric.label}
                label={metric.label}
                value={metric.value}
                tone={normalizeTone(metric.tone)}
                hint={metric.hint || (metric.label === selectedMetric ? 'Selected metric' : undefined)}
              />
            ))}
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <Panel className="xl:col-span-2" eyebrow="SUMMARY" title={lane.summary} toneAccent={TONE_ACCENT[lane.tone]}>
              <div className="grid gap-3 md:grid-cols-2">
                <DetailBlock icon={<ListChecks />} title="Current state" body={`${lane.score}/100 status score. ${lane.summary}.`} tone={lane.tone} />
                <DetailBlock icon={<ClipboardList />} title="Recommended action" body={recommendedAction(lane, overview)} tone={lane.tone} />
              </div>
            </Panel>

            <Panel eyebrow="SOURCE" title="Filtered source view" toneAccent="brand">
              <p className="text-sm text-text-secondary">
                Source page keeps the operational controls and raw records for this lane.
              </p>
              <Button asChild className="mt-3" size="sm">
                <Link to={sourceRoute}>
                  Open source view
                  <ArrowRight />
                </Link>
              </Button>
            </Panel>
          </div>

          {lane.id === 'ip-behavior' ? (
            <Panel eyebrow="COUNTRIES" title="Country behavior table" toneAccent={overview.ip_behavior.findings.length > 0 ? 'warning' : 'healthy'}>
              <IPBehaviorCountryTable overview={overview} />
            </Panel>
          ) : null}

          {lane.id === 'exposure' ? <ExposurePosturePanel overview={overview} onRefresh={refresh} /> : null}

          {lane.id === 'app-db-health' ? <AppDBPosturePanel overview={overview} lane={lane} /> : null}

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <Panel className="xl:col-span-2" eyebrow="EVIDENCE" title="Evidence rows" toneAccent={TONE_ACCENT[lane.tone]}>
              {lane.items?.length ? (
                <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
                  {lane.items.map((item) => (
                    <Link
                      key={`${item.label}:${item.value}:${item.hint}`}
                      to={item.drilldown || sourceRoute}
                      className="flex items-start justify-between gap-4 p-3 hover:bg-hover"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-foreground">{item.label}</p>
                        <p className="mt-1 text-xs text-text-secondary">{item.hint || 'Evidence linked to this lane.'}</p>
                      </div>
                      <StatusTag tone={normalizeTone(item.tone)}>{item.value}</StatusTag>
                    </Link>
                  ))}
                </div>
              ) : (
                <EmptyState icon={<ShieldCheck />} title="No evidence rows" description="No lane evidence rows for this period." />
              )}
            </Panel>

            <Panel eyebrow="AFFECTED" title="Affected assets" toneAccent={TONE_ACCENT[lane.tone]}>
              {affectedAssets(lane).length ? (
                <div className="flex flex-col gap-2">
                  {affectedAssets(lane).map((asset) => (
                    <Link key={asset.label} to={asset.to} className="rounded-lg border border-border-subtle bg-surface p-3 text-sm hover:bg-hover">
                      <span className="font-medium text-foreground">{asset.label}</span>
                      <span className="ml-2 text-xs text-text-muted">{asset.value}</span>
                    </Link>
                  ))}
                </div>
              ) : (
                <EmptyState title="No affected assets" description="This lane has no linked affected assets in the current window." />
              )}
            </Panel>
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <Panel eyebrow="TIMELINE" title="Recent signals" toneAccent="warning">
              <TimelineRows incidents={laneIncidents(lane, overview)} />
            </Panel>
            <Panel eyebrow="ACTIONS" title="Action queue" toneAccent={laneActions(lane, overview).some((a) => a.count > 0) ? 'warning' : 'healthy'}>
              <ActionRows actions={laneActions(lane, overview)} />
            </Panel>
            <Panel eyebrow="AUDIT" title="Recent audit path" toneAccent="brand">
              <div className="flex items-start gap-3 rounded-lg border border-border-subtle bg-surface p-3">
                <History className="mt-0.5 h-4 w-4 text-text-muted" />
                <div>
                  <p className="text-sm font-medium text-foreground">Audit trail</p>
                  <p className="mt-1 text-xs text-text-secondary">Open audit filtered by actions from this lane.</p>
                  <Button asChild variant="ghost" size="sm" className="mt-2">
                    <Link to="/audit">
                      Open audit
                      <ArrowRight />
                    </Link>
                  </Button>
                </div>
              </div>
            </Panel>
          </div>
        </>
      ) : null}
    </div>
  );
}

function DetailBlock({ icon, title, body, tone }: { icon: JSX.Element; title: string; body: string; tone: string }) {
  return (
    <div className="flex gap-3 rounded-lg border border-border-subtle bg-surface p-3">
      <span className={`mt-0.5 ${toneText(tone)} [&_svg]:h-4 [&_svg]:w-4`}>{icon}</span>
      <div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="mt-1 text-sm text-text-secondary">{body}</p>
      </div>
    </div>
  );
}

function MetricText({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-surface px-2.5 py-2">
      <p className="text-[0.68rem] uppercase tracking-wide text-text-muted">{label}</p>
      <p className="mt-0.5 font-mono text-sm font-semibold tabular-nums text-foreground">{value}</p>
    </div>
  );
}

function TimelineRows({ incidents }: { incidents: ControlRoomIncident[] }) {
  if (!incidents.length) {
    return <EmptyState icon={<FileText />} title="No recent incidents" description="No incidents are linked to this lane in the selected period." />;
  }
  return (
    <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle">
      {incidents.slice(0, 6).map((incident) => (
        <Link key={`${incident.source}:${incident.id}`} to={incident.drilldown || '/alerts'} className="block p-3 hover:bg-hover">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-foreground">{incident.title}</p>
              <p className="mt-1 text-xs text-text-secondary">{incident.summary || incident.source}</p>
            </div>
            <StatusTag tone={severityToTone(incident.severity)}>{incident.severity}</StatusTag>
          </div>
        </Link>
      ))}
    </div>
  );
}

function ActionRows({ actions }: { actions: ControlRoomAction[] }) {
  if (!actions.some((action) => action.count > 0)) {
    return <EmptyState icon={<ShieldCheck />} title="No queued actions" description="No approvals or enforcement actions are waiting for this lane." />;
  }
  return (
    <div className="flex flex-col gap-2">
      {actions.map((action) => (
        <Link key={action.id} to={action.drilldown} className="flex items-center justify-between rounded-lg border border-border-subtle bg-surface p-3 hover:bg-hover">
          <span className="text-sm font-medium text-foreground">{action.label}</span>
          <StatusTag tone={normalizeTone(action.tone)}>{action.count}</StatusTag>
        </Link>
      ))}
    </div>
  );
}

function IPBehaviorCountryTable({ overview }: { overview: ControlRoomOverview }) {
  const countries = overview.ip_behavior.countries ?? [];
  if (!countries.length) {
    return <EmptyState title="No country traffic" description="No web.request country rollups in this period." />;
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-border-subtle">
      <table className="w-full text-sm">
        <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
          <tr>
            <th className="px-3 py-2">Country</th>
            <th className="px-3 py-2">Requests</th>
            <th className="px-3 py-2">Unique IPs</th>
            <th className="px-3 py-2">Bytes out</th>
            <th className="px-3 py-2">401/403</th>
            <th className="px-3 py-2">5xx</th>
            <th className="px-3 py-2">ASN/app/group</th>
          </tr>
        </thead>
        <tbody>
          {countries.slice(0, 10).map((country) => {
            const auth = (country.status_counts?.['401'] ?? 0) + (country.status_counts?.['403'] ?? 0);
            const serverErrors = (country.status_counts?.['500'] ?? 0) + (country.status_counts?.['502'] ?? 0) + (country.status_counts?.['503'] ?? 0);
            return (
              <tr key={country.country_code || country.country} className="border-t border-border-subtle">
                <td className="px-3 py-2">
                  <Link to={`/security/network?tab=ip-behavior&country=${encodeURIComponent(country.country_code || country.country)}`} className="font-medium text-brand-400 hover:underline">
                    {country.country || country.country_code || 'unknown'}
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono tabular-nums">{formatNumber(country.request_count)}</td>
                <td className="px-3 py-2 font-mono tabular-nums">{formatNumber(country.unique_source_ips)}</td>
                <td className="px-3 py-2 font-mono tabular-nums">{formatBytes(country.bytes_out)}</td>
                <td className="px-3 py-2 font-mono tabular-nums">{formatNumber(auth)}</td>
                <td className="px-3 py-2 font-mono tabular-nums">{formatNumber(serverErrors)}</td>
                <td className="max-w-[20rem] truncate px-3 py-2 text-text-secondary">
                  {compactList([...(country.top_asns ?? []), ...(country.top_apps ?? []), ...(country.server_groups ?? [])]) || 'not reported'}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function ExposurePosturePanel({ overview, onRefresh }: { overview: ControlRoomOverview; onRefresh: () => Promise<void> }) {
  const api = useApiClient();
  const firewall = overview.firewall;
  const isolation = overview.isolation;
  const firewallGaps = firewall.unknown + firewall.disabled;
  const hasGaps = firewallGaps > 0 || firewall.stale > 0 || isolation.whitelist_gaps > 0 || isolation.expired > 0;
  const nodes = firewall.nodes ?? [];
  const publicListeners = overview.exposure?.public_listeners ?? [];
  const exposureGaps = publicListeners.filter((row) => publicListenerIsGap(row));
  const gapNodeIds = uniqueStrings(exposureGaps.map((row) => row.node_id));
  const [actionState, setActionState] = useState<Record<string, string>>({});

  const runIsolationAction = async (
    nodeIds: string[],
    mode: NetworkIsolationMode,
    durationSeconds: number,
    label: string,
  ) => {
    if (nodeIds.length === 0) return;
    const verb = mode === 'airgapped' ? 'airgap' : 'put in whitelist-only mode';
    if (!window.confirm(`Confirm ${verb} for ${nodeIds.length} node${nodeIds.length === 1 ? '' : 's'} (${label})?`)) return;
    const key = `${mode}:${nodeIds.join(',')}`;
    setActionState((current) => ({ ...current, [key]: 'Updating...' }));
    try {
      for (const nodeId of nodeIds) {
        await api.setNodeIsolation(nodeId, {
          mode,
          duration_seconds: durationSeconds,
          reason: mode === 'airgapped'
            ? 'Emergency exposure containment from Control Room'
            : 'Exposure remediation from Control Room',
          allowed_applications: mode === 'whitelist' ? ['control-one-agent', 'patch'] : undefined,
        });
      }
      setActionState((current) => ({ ...current, [key]: 'Updated' }));
      await onRefresh();
    } catch (err) {
      setActionState((current) => ({
        ...current,
        [key]: err instanceof Error ? err.message : 'Update failed',
      }));
    }
  };

  return (
    <Panel eyebrow="FIREWALL AND ISOLATION" title="Exposure protection posture" toneAccent={hasGaps ? 'warning' : 'healthy'}>
      <p className="max-w-4xl text-sm text-text-secondary">
        Public services stay in exposure until Control One sees an active isolation mode or a default-deny host firewall protecting the node.
      </p>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-6">
        <KpiTile size="sm" label="Firewall active" value={formatNumber(firewall.enabled)} tone={firewall.enabled > 0 ? 'healthy' : 'unknown'} />
        <KpiTile size="sm" label="Default deny" value={formatNumber(firewall.default_deny)} tone={firewall.default_deny > 0 ? 'healthy' : 'warning'} />
        <KpiTile size="sm" label="Unknown/off" value={formatNumber(firewallGaps)} tone={firewallGaps > 0 ? 'warning' : 'healthy'} />
        <KpiTile size="sm" label="Stale firewall" value={formatNumber(firewall.stale)} tone={firewall.stale > 0 ? 'warning' : 'healthy'} />
        <KpiTile size="sm" label="Isolation protected" value={formatNumber(isolation.protected)} tone={isolation.protected > 0 ? 'healthy' : 'unknown'} />
        <KpiTile size="sm" label="Whitelist gaps" value={formatNumber(isolation.whitelist_gaps)} tone={isolation.whitelist_gaps > 0 ? 'warning' : 'healthy'} />
      </div>

      <div className="grid gap-3 xl:grid-cols-[1fr_22rem]">
        <div className="rounded-lg border border-border-subtle bg-surface p-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p className="text-xs uppercase tracking-wide text-text-muted">Path to 100%</p>
              <h4 className="mt-1 text-base font-semibold text-foreground">
                {exposureGaps.length > 0
                  ? `Protect ${exposureGaps.length} public listener${exposureGaps.length === 1 ? '' : 's'} across ${gapNodeIds.length} node${gapNodeIds.length === 1 ? '' : 's'}`
                  : 'All reported public listeners have a protection signal'}
              </h4>
              <p className="mt-1 max-w-3xl text-sm text-text-secondary">
                The fastest safe path is whitelist-only containment with Control One agent and patch access allowed. The durable fix is default-deny inbound firewall policy with explicit allow rules for approved services.
              </p>
              <p className="mt-1 max-w-3xl text-xs text-text-muted">
                Posture-template equivalent: apply a TTL emergency override now, then promote a scoped moderate or aggressive ingress template after previewing affected ports, egress allowlists, and drift.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant="primary"
                disabled={gapNodeIds.length === 0}
                onClick={() => void runIsolationAction(gapNodeIds, 'whitelist', 24 * 60 * 60, 'all exposure gaps')}
              >
                <LockKeyhole />
                Apply whitelist
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={gapNodeIds.length === 0}
                onClick={() => void runIsolationAction(gapNodeIds, 'airgapped', 60 * 60, 'all exposure gaps')}
              >
                <WifiOff />
                Airgap 1h
              </Button>
            </div>
          </div>
          {(['whitelist', 'airgapped'] as const).map((mode) => {
            const status = actionState[`${mode}:${gapNodeIds.join(',')}`];
            return status ? (
              <p key={mode} className="mt-2 text-xs text-text-muted">
                {mode === 'whitelist' ? 'Apply whitelist' : 'Airgap 1h'}: {status}
              </p>
            ) : null;
          })}
        </div>

        <div className="rounded-lg border border-border-subtle bg-elevated p-4">
          <p className="text-xs uppercase tracking-wide text-text-muted">Executive readout</p>
          <p className="mt-2 text-sm text-text-secondary">
            {publicListeners.length === 0
              ? 'No public listeners are reported in this window.'
              : `${formatNumber(publicListeners.length)} public listener${publicListeners.length === 1 ? '' : 's'} reported; ${formatNumber(exposureGaps.length)} still need a stronger protection signal.`}
          </p>
          <div className="mt-3 grid grid-cols-2 gap-2">
            <MetricText label="Protected" value={formatNumber(publicListeners.length - exposureGaps.length)} />
            <MetricText label="Needs action" value={formatNumber(exposureGaps.length)} />
          </div>
        </div>
      </div>

      <div className="overflow-x-auto rounded-lg border border-border-subtle">
        <table className="w-full text-sm">
          <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
            <tr>
              <th className="px-3 py-2">Public listener</th>
              <th className="px-3 py-2">Exposure</th>
              <th className="px-3 py-2">Protection signal</th>
              <th className="px-3 py-2">Recommended action</th>
              <th className="px-3 py-2 text-right">Contain</th>
            </tr>
          </thead>
          <tbody>
            {publicListeners.length === 0 ? (
              <tr>
                <td className="px-3 py-4 text-text-secondary" colSpan={5}>
                  No public listeners reported in this period.
                </td>
              </tr>
            ) : publicListeners.slice(0, 50).map((listener) => {
              const rowGap = publicListenerIsGap(listener);
              const whitelistKey = `whitelist:${listener.node_id}`;
              const airgapKey = `airgapped:${listener.node_id}`;
              return (
                <tr key={`${listener.node_id}:${listener.listen_addr}:${listener.port}:${listener.process}`} className="border-t border-border-subtle">
                  <td className="px-3 py-2">
                    <Link to={`/nodes/${listener.node_id}`} className="font-medium text-brand-400 hover:underline">
                      {listener.hostname || listener.node_id}
                    </Link>
                    <p className="mt-0.5 font-mono text-xs text-text-muted">
                      {publicListenerName(listener)}
                    </p>
                  </td>
                  <td className="px-3 py-2">
                    <StatusTag tone={normalizeTone(listener.tone)}>{rowGap ? 'needs action' : 'protected'}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-text-secondary">{listener.protection}</td>
                  <td className="max-w-[28rem] px-3 py-2 text-text-secondary">{listener.recommended_action}</td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap justify-end gap-2">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={!rowGap}
                        onClick={() => void runIsolationAction([listener.node_id], 'whitelist', 24 * 60 * 60, listener.hostname)}
                      >
                        <LockKeyhole />
                        24h
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        disabled={!rowGap}
                        onClick={() => void runIsolationAction([listener.node_id], 'airgapped', 60 * 60, listener.hostname)}
                      >
                        <WifiOff />
                        1h
                      </Button>
                    </div>
                    {([
                      ['whitelist', actionState[whitelistKey]],
                      ['airgapped', actionState[airgapKey]],
                    ] as const).map(([mode, status]) => (
                      status ? <p key={mode} className="mt-1 text-right text-xs text-text-muted">{status}</p> : null
                    ))}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {nodes.length ? (
        <div className="overflow-x-auto rounded-lg border border-border-subtle">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
              <tr>
                <th className="px-3 py-2">Node</th>
                <th className="px-3 py-2">Firewall</th>
                <th className="px-3 py-2">State</th>
                <th className="px-3 py-2">Exposure effect</th>
                <th className="px-3 py-2">Observed</th>
              </tr>
            </thead>
            <tbody>
              {nodes.slice(0, 12).map((node) => (
                <tr key={node.node_id} className="border-t border-border-subtle">
                  <td className="px-3 py-2">
                    <Link to={`/nodes/${node.node_id}`} className="font-medium text-brand-400 hover:underline">
                      {node.hostname || node.node_id}
                    </Link>
                  </td>
                  <td className="px-3 py-2 text-text-secondary">{node.firewall_type || 'not reported'}</td>
                  <td className="px-3 py-2">
                    <StatusTag tone={firewallNodeTone(node)}>{firewallNodeState(node)}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-text-secondary">{firewallNodeEffect(node)}</td>
                  <td className="px-3 py-2 text-text-secondary">{formatDateTime(node.observed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <EmptyState
          icon={<ShieldCheck />}
          title="No firewall posture reported"
          description="Node agents have not reported host firewall state in this period."
        />
      )}
    </Panel>
  );
}

function AppDBPosturePanel({ overview, lane }: { overview: ControlRoomOverview; lane: ControlRoomLane }) {
  const [filter, setFilter] = useState<AppDBFilter>('all');
  const appRows = appDBApplicationRows(overview.webservers.instances, overview.isolation.nodes);
  const filteredRows = appRows.filter((row) => appDBFilterMatches(row, filter));
  const captureGaps = Math.max(0, overview.webservers.total - overview.webservers.capture_ready);
  const skillRequired = appRows.filter((row) => row.status === 'skill_required').length;
  const purposes = uniqueStrings(overview.webservers.instances.flatMap((instance) => stringListField(instance.capabilities, 'server_purposes')));
  const databaseMetric = lane.metrics.find((metric) => metric.label === 'Databases')?.value ?? '0';
  const filters: Array<{ id: AppDBFilter; label: string; count: number }> = [
    { id: 'all', label: 'All', count: appRows.length },
    { id: 'skills', label: 'Skills needed', count: appRows.filter(appDBNeedsSkill).length },
    { id: 'generic', label: 'Generic parser', count: appRows.filter(appDBGenericParser).length },
    { id: 'unsupported_dbms', label: 'Unsupported DBMS', count: appRows.filter(appDBUnsupportedDBMS).length },
    { id: 'direct', label: 'Direct edge', count: appRows.filter(appDBDirectExposure).length },
    { id: 'offline', label: 'Airgap bundle gaps', count: appRows.filter(appDBOfflineBundleGap).length },
  ];

  return (
    <Panel eyebrow="APP AND DB CONTEXT" title="Detected apps, DBMS, and parser coverage" toneAccent={captureGaps > 0 || skillRequired > 0 ? 'warning' : 'healthy'}>
      <p className="max-w-4xl text-sm text-text-secondary">
        Server purpose and application root detection come from package inventory plus webserver config analysis. Parser gaps stay visible until a matching skill or capture profile is assigned.
      </p>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-5">
        <KpiTile size="sm" label="Webservers" value={formatNumber(overview.webservers.total)} tone={overview.webservers.total > 0 ? 'info' : 'unknown'} />
        <KpiTile size="sm" label="Capture gaps" value={formatNumber(captureGaps)} tone={captureGaps > 0 ? 'warning' : 'healthy'} />
        <KpiTile size="sm" label="App roots" value={formatNumber(appRows.length)} tone={appRows.length > 0 ? 'info' : 'unknown'} />
        <KpiTile size="sm" label="Skills needed" value={formatNumber(skillRequired)} tone={skillRequired > 0 ? 'warning' : 'healthy'} />
        <KpiTile size="sm" label="DB services" value={databaseMetric} tone={databaseMetric === '0' ? 'unknown' : 'info'} />
      </div>

      <div className="flex flex-wrap gap-2">
        {filters.map((item) => (
          <Button
            key={item.id}
            type="button"
            size="sm"
            variant={filter === item.id ? 'primary' : 'outline'}
            onClick={() => setFilter(item.id)}
          >
            {item.label}
            <span className="ml-1 text-xs opacity-75">{formatNumber(item.count)}</span>
          </Button>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-[1fr_18rem]">
        <div className="overflow-x-auto rounded-lg border border-border-subtle">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
              <tr>
                <th className="px-3 py-2">Instance</th>
                <th className="px-3 py-2">App/vhost</th>
                <th className="px-3 py-2">Root</th>
                <th className="px-3 py-2">Parser skill</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.length === 0 ? (
                <tr>
                  <td className="px-3 py-3 text-text-secondary" colSpan={4}>
                    {appRows.length === 0 ? 'No application roots reported by webserver inventory.' : 'No app roots match this coverage filter.'}
                  </td>
                </tr>
              ) : filteredRows.slice(0, 12).map((row) => (
                <tr key={`${row.instanceID}:${row.name}:${row.path}`} className="border-t border-border-subtle">
                  <td className="px-3 py-2">
                    <Link to="/security/webservers" className="font-medium text-brand-400 hover:underline">
                      {row.kind} {row.service || 'default'}
                    </Link>
                    <p className="text-xs text-text-muted">{row.airgapped ? 'airgapped' : row.enforceReady ? 'edge controls available' : 'edge controls not ready'}</p>
                  </td>
                  <td className="px-3 py-2">
                    <p className="font-medium text-foreground">{row.name}</p>
                    <p className="text-xs text-text-muted">
                      {row.type}{row.confidence ? ` - ${row.confidence}% confidence` : ''}
                    </p>
                  </td>
                  <td className="max-w-[22rem] px-3 py-2 text-xs text-text-secondary">
                    <p className="truncate font-mono">{row.path || 'not reported'}</p>
                    <p className="truncate">{compactList(row.evidence, 2) || 'evidence not reported'}</p>
                  </td>
                  <td className="px-3 py-2">
                    <StatusTag tone={row.status === 'skill_required' ? 'warning' : row.status ? 'healthy' : 'unknown'}>
                      {row.skill || row.status || 'unknown'}
                    </StatusTag>
                    {row.remediationSkill && <p className="mt-1 text-xs text-text-muted">{row.remediationSkill}</p>}
                    <p className="mt-1 text-xs text-text-muted">{row.captureReady ? 'capture ready' : 'capture gap'}</p>
                    {row.catalogVersion && <p className="mt-1 text-xs text-text-muted">{row.catalogVersion}</p>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="flex flex-col gap-3 rounded-lg border border-border-subtle bg-surface p-3">
          <div>
            <p className="text-xs uppercase tracking-wide text-text-muted">Inferred purposes</p>
            <p className="mt-1 text-sm text-text-secondary">{compactList(purposes) || 'No webserver purpose labels reported.'}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wide text-text-muted">Next action</p>
            <p className="mt-1 text-sm text-text-secondary">
              {skillRequired > 0 ? 'Assign parser skills for the app roots marked as skill required.' : captureGaps > 0 ? 'Enable managed capture on webservers with capture gaps.' : 'App and web capture signals are covered for reported instances.'}
            </p>
          </div>
          <Button asChild variant="outline" size="sm">
            <Link to="/security/webservers">
              Open webserver control
              <ArrowRight />
            </Link>
          </Button>
        </div>
      </div>
    </Panel>
  );
}

function detailDescription(tenantName: string | undefined, overview: ControlRoomOverview | null, lane: ControlRoomLane): string {
  const prefix = tenantName ? `${tenantName}: ` : '';
  return `${prefix}${lane.summary}. ${overview?.period ?? '24h'} window, generated ${formatDateTime(overview?.generated_at)}.`;
}

function laneIncidents(lane: ControlRoomLane, overview: ControlRoomOverview): ControlRoomIncident[] {
  const sources = LANE_SOURCES[lane.id] || [];
  if (!sources.length) return [];
  return overview.top_incidents.filter((incident) => sources.some((source) => incident.source.includes(source)));
}

function laneActions(lane: ControlRoomLane, overview: ControlRoomOverview): ControlRoomAction[] {
  switch (lane.id) {
    case 'ip-behavior':
      return overview.pending_actions.filter((action) => action.id === 'ip-findings' || action.id === 'block-enforcement');
    case 'patch-posture':
      return overview.pending_actions.filter((action) => action.id === 'patch-approvals');
    case 'exposure':
      return overview.pending_actions.filter((action) => action.id === 'block-enforcement' || action.id === 'isolation-posture');
    default:
      return overview.pending_actions.filter((action) => action.count > 0);
  }
}

function affectedAssets(lane: ControlRoomLane): Array<{ label: string; value: string; to: string }> {
  return (lane.items ?? [])
    .filter((item) => item.drilldown)
    .slice(0, 5)
    .map((item) => ({ label: item.label, value: item.value, to: item.drilldown || lane.drilldown }));
}

function recommendedAction(lane: ControlRoomLane, overview: ControlRoomOverview): string {
  if (lane.tone === 'healthy') return 'No operator action is waiting for this lane.';
  if (lane.id === 'ip-behavior') {
    const finding = overview.ip_behavior.findings[0];
    if (!finding) return 'Review open IP findings.';
    const presentation = describeIPBehaviorFinding(finding, { countryLabel: finding.country_code, maxSignals: 3 });
    const signals = presentation.signals.length > 0 ? `: ${presentation.signals.slice(0, 3).join(', ')}` : '';
    return `Review ${presentation.source}: ${presentation.confidence}% confidence ${presentation.categoryLabel.toLowerCase()}${signals}.`;
  }
  if (lane.id === 'patch-posture') return 'Review failed deployments and pending approvals before the next maintenance window.';
  if (lane.id === 'exposure') return 'Review public listeners that lack airgap, whitelist mode, or default-deny firewall protection.';
  if (lane.id === 'app-db-health') return 'Review missing web capture, DB probe errors, and app/DB service coverage.';
  if (lane.id === 'server-health') return 'Review stale or offline agents before trusting downstream telemetry.';
  return 'Review linked incidents and evidence rows.';
}

function firewallNodeTone(node: ControlRoomFirewallNode): StateTone {
  if (!node.known || !node.enabled || node.stale) return 'warning';
  if (node.default_deny) return 'healthy';
  return 'info';
}

function firewallNodeState(node: ControlRoomFirewallNode): string {
  if (!node.known) return 'unknown';
  if (!node.enabled) return 'off';
  if (node.stale) return 'stale';
  if (node.default_deny) return 'default deny';
  return 'enabled';
}

function firewallNodeEffect(node: ControlRoomFirewallNode): string {
  if (!node.known) return 'firewall state unknown';
  if (!node.enabled) return 'not reducing exposure';
  if (node.stale) return 'needs fresh agent report';
  if (node.default_deny) return 'counts as protected';
  return 'rules active, default allow';
}

function publicListenerIsGap(listener: ControlRoomPublicListener): boolean {
  return !listener.exposure_state.startsWith('protected_');
}

function publicListenerName(listener: ControlRoomPublicListener): string {
  const service = listener.service_kind || listener.process || 'listener';
  return `${service} on ${formatListenAddress(listener.listen_addr, listener.port)}`;
}

function formatListenAddress(addr: string, port: number): string {
  const trimmed = addr.trim();
  if (trimmed === '::' || trimmed === '[::]') return `[::]:${port}`;
  if (trimmed.includes(':') && !trimmed.startsWith('[')) return `[${trimmed}]:${port}`;
  return `${trimmed || '0.0.0.0'}:${port}`;
}

interface AppDBApplicationRow {
  instanceID: string;
  nodeID: string;
  kind: string;
  service: string;
  name: string;
  type: string;
  path: string;
  status: string;
  skill: string;
  remediationSkill: string;
  confidence: string;
  catalogVersion: string;
  evidence: string[];
  captureReady: boolean;
  enforceReady: boolean;
  airgapped: boolean;
}

type AppDBFilter = 'all' | 'skills' | 'generic' | 'unsupported_dbms' | 'direct' | 'offline';

function appDBApplicationRows(instances: ControlRoomWebserver[], isolationNodes: ControlRoomIsolationNode[]): AppDBApplicationRow[] {
  const out: AppDBApplicationRow[] = [];
  const isolationByNode = new Map(isolationNodes.map((node) => [node.id, node]));
  for (const instance of instances) {
    const isolation = isolationByNode.get(instance.node_id);
    for (const [index, vhost] of (instance.vhosts ?? []).entries()) {
      out.push({
        instanceID: instance.id,
        nodeID: instance.node_id,
        kind: instance.kind,
        service: instance.service,
        name: stringField(vhost, 'name', 'vhost', 'server_name') || `app ${index + 1}`,
        type: stringField(vhost, 'application_name', 'application_type', 'app_type') || 'unknown',
        path: stringField(vhost, 'document_root', 'path', 'root'),
        status: stringField(vhost, 'coverage_state', 'log_skill_status'),
        skill: stringField(vhost, 'parser_profile_id', 'suggested_skill'),
        remediationSkill: stringField(vhost, 'remediation_skill_id'),
        confidence: stringField(vhost, 'confidence'),
        catalogVersion: stringField(vhost, 'catalog_version'),
        evidence: stringListField(vhost, 'evidence'),
        captureReady: instance.capture_ready,
        enforceReady: instance.enforce_ready,
        airgapped: Boolean(isolation?.active && isolation.mode === 'airgapped'),
      });
    }
  }
  return out;
}

function appDBFilterMatches(row: AppDBApplicationRow, filter: AppDBFilter): boolean {
  switch (filter) {
    case 'skills':
      return appDBNeedsSkill(row);
    case 'generic':
      return appDBGenericParser(row);
    case 'unsupported_dbms':
      return appDBUnsupportedDBMS(row);
    case 'direct':
      return appDBDirectExposure(row);
    case 'offline':
      return appDBOfflineBundleGap(row);
    default:
      return true;
  }
}

function appDBNeedsSkill(row: AppDBApplicationRow): boolean {
  const status = row.status.toLowerCase();
  const skill = `${row.skill} ${row.remediationSkill}`.toLowerCase();
  return status === 'skill_required' || status === 'missing_skill' || skill.includes('custom');
}

function appDBGenericParser(row: AppDBApplicationRow): boolean {
  const status = row.status.toLowerCase();
  const skill = row.skill.toLowerCase();
  return status === 'generic_access_log' || skill.includes('generic');
}

function appDBUnsupportedDBMS(row: AppDBApplicationRow): boolean {
  const status = row.status.toLowerCase();
  const type = row.type.toLowerCase();
  return status === 'unsupported_dbms' || (type.includes('database') && appDBNeedsSkill(row));
}

function appDBDirectExposure(row: AppDBApplicationRow): boolean {
  return row.enforceReady && !row.airgapped;
}

function appDBOfflineBundleGap(row: AppDBApplicationRow): boolean {
  return row.airgapped && appDBNeedsSkill(row);
}

function stringField(row: Record<string, unknown> | undefined, ...keys: string[]): string {
  if (!row) return '';
  for (const key of keys) {
    const value = row[key];
    if (typeof value === 'string') return value.trim();
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  }
  return '';
}

function stringListField(row: Record<string, unknown> | undefined, key: string): string[] {
  const value = row?.[key];
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean);
  if (typeof value === 'string') return value.split(',').map((item) => item.trim()).filter(Boolean);
  return [];
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed || seen.has(trimmed.toLowerCase())) continue;
    seen.add(trimmed.toLowerCase());
    out.push(trimmed);
  }
  return out;
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

function compactList(values?: Array<string | undefined | null>, limit = 4): string {
  return (values ?? [])
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value))
    .slice(0, limit)
    .join(', ');
}

export default ControlRoomDrilldown;
