import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { AlertTriangle, ArrowRight, ShieldCheck } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Alert, Panel, SectionHeader, EmptyState, KpiTile, StatusTag, type StateTone } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { DashboardGrid, DashboardGridItem } from '@/components/shell';
import {
  EntityHeader,
  InvestigateTimeline,
  IpEnrichmentCard,
  IpLifecyclePanel,
  RelatedEntities,
} from '@/components/investigate';
import { useApiClient } from '@/hooks/useApiClient';
import { useRolePick } from '@/hooks/useRolePick';
import { useTenant } from '@/providers/TenantProvider';
import { toast } from 'sonner';
import { ENTITY_TYPE_LABELS } from '@/lib/entity';
import { describeIPBehaviorFinding, ipBehaviorConfidence } from '@/lib/ipBehaviorPresentation';
import { formatBytes, formatTs } from '@/lib/format';
import type { EntityType } from '@/components/kit';
import type {
  BehavioralAnomaly,
  EntityDetail as EntityDetailData,
  EntityLifecycle,
  EntityRelated,
  IpEnrichment,
  InvestigationTimelineItem,
  IPBehaviorIPProfile,
  LifecycleItem,
} from '@/lib/api';

const VALID_TYPES: EntityType[] = [
  'ip', 'process', 'file', 'hash', 'user', 'host', 'domain', 'url', 'session', 'alert', 'rule', 'tenant',
];

export function EntityDetail(): JSX.Element {
  const { type, id } = useParams<{ type: string; id: string }>();
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const { isAdmin, isOperator } = useRolePick();
  const canMutate = isAdmin || isOperator;
  const [tab, setTab] = useState(type === 'ip' ? 'connections' : 'timeline');
  const [cursor, setCursor] = useState<string | undefined>();
  const [accumulated, setAccumulated] = useState<LifecycleItem[]>([]);
  const ipSince = useMemo(() => new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(), []);
  const ipTimelineSince = useMemo(() => new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(), []);

  const safeType = (VALID_TYPES.includes(type as EntityType) ? type : 'ip') as EntityType;

  const detailQ = useQuery<EntityDetailData>({
    queryKey: ['entity', currentTenantId, safeType, id],
    queryFn: () => client.getEntity(safeType, id ?? '', { tenantId: currentTenantId }),
    enabled: !!id && !!currentTenantId,
  });

  const lifecycleQ = useQuery<EntityLifecycle>({
    queryKey: ['entity.lifecycle', currentTenantId, safeType, id, cursor],
    queryFn: () =>
      client.getEntityLifecycle(safeType, id ?? '', { tenantId: currentTenantId, cursor, limit: 50 }),
    enabled: !!id && !!currentTenantId,
  });

  // Reset accumulator when entity changes (cursor clears).
  useEffect(() => {
    setAccumulated([]);
    setCursor(undefined);
  }, [safeType, id]);

  // Accumulate lifecycle items as pages arrive.
  useEffect(() => {
    if (!lifecycleQ.data) return;
    if (!cursor) {
      setAccumulated(lifecycleQ.data.items);
    } else {
      setAccumulated((prev) => [...prev, ...lifecycleQ.data.items]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lifecycleQ.data]);

  const ipTimelineQ = useQuery({
    queryKey: ['entity.ip.timeline', currentTenantId, id, ipTimelineSince],
    queryFn: () =>
      client.buildInvestigationTimeline({
        tenantId: currentTenantId,
        entityType: 'ip',
        entityId: id ?? '',
        since: ipTimelineSince,
        limit: 100,
      }),
    enabled: safeType === 'ip' && !!id && !!currentTenantId,
    staleTime: 30_000,
  });

  const relatedQ = useQuery<EntityRelated>({
    queryKey: ['entity.related', currentTenantId, safeType, id],
    queryFn: () => client.getEntityRelated(safeType, id ?? '', { tenantId: currentTenantId }),
    enabled: !!id && !!currentTenantId,
  });

  const ipEnrichQ = useQuery<IpEnrichment>({
    queryKey: ['entity.ip.enrich', id, currentTenantId],
    queryFn: () => client.enrichIp(id ?? '', currentTenantId),
    enabled: !!id && safeType === 'ip' && !!currentTenantId,
  });

  const ipBehaviorProfileQ = useQuery<IPBehaviorIPProfile>({
    queryKey: ['entity.ip.behavior.profile', currentTenantId, id, ipSince],
    queryFn: () => client.getIPBehaviorIPProfile({ tenantId: currentTenantId ?? '', ip: id ?? '', since: ipSince }),
    enabled: !!id && safeType === 'ip' && !!currentTenantId,
  });

  const ipBehaviorFindingsQ = useQuery({
    queryKey: ['entity.ip.behavior.findings', currentTenantId, id],
    queryFn: () => client.listAnomalies({ tenantId: currentTenantId ?? '', sourceIp: id ?? '', resolved: false, limit: 10 }),
    enabled: !!id && safeType === 'ip' && !!currentTenantId,
  });
  const ipBehaviorFindings = ipBehaviorFindingsQ.data?.data ?? [];
  const smallTimelineItems = useMemo(
    () => (ipTimelineQ.data?.items ?? []).map(investigationTimelineItemToLifecycle),
    [ipTimelineQ.data?.items],
  );
  const lifecycleItems = useMemo(
    () => mergeLifecycleItems(accumulated, smallTimelineItems),
    [accumulated, smallTimelineItems],
  );
  const lifecycleLoading =
    lifecycleQ.isLoading ||
    (safeType === 'ip' && ipTimelineQ.isLoading && lifecycleItems.length === 0);
  const headerDetail = useMemo(
    () => detailWithLifecycleEventCount(detailQ.data, lifecycleItems),
    [detailQ.data, lifecycleItems],
  );

  const onAction = async (action: 'block' | 'allow' | 'quarantine') => {
    if (!currentTenantId) {
      toast.error('Select a tenant first');
      return;
    }
    try {
      const payload =
        safeType === 'ip' && (action === 'block' || action === 'allow')
          ? { action, scope: 'fleet' as const, reason: `Manual ${action} from investigation` }
          : { action };
      const resp = await client.entityAction(safeType, id ?? '', payload, { tenantId: currentTenantId });
      if (safeType === 'ip' && (action === 'block' || action === 'allow')) {
        const verb = action === 'block' ? 'Block' : 'Allow';
        const dispatched = resp.nodes_dispatched ?? 0;
        if (dispatched > 0) {
          toast.success(`${verb} dispatched to ${dispatched} server${dispatched === 1 ? '' : 's'}`);
        } else {
          toast.warning(`${verb} recorded, but no enrolled server received it`);
        }
      } else {
        toast.success(`${action} action queued`);
      }
      void detailQ.refetch();
      void lifecycleQ.refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Action failed');
    }
  };

  if (!id) {
    return (
      <SectionHeader title="Entity not found" description="Missing identifier." />
    );
  }

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow={`INVESTIGATE · ${ENTITY_TYPE_LABELS[safeType].toUpperCase()}`}
        title="Entity lifecycle"
        description="Every event, alert, audit and remediation touching this entity."
      />

      <EntityHeader
        type={safeType}
        id={id}
        detail={headerDetail}
        loading={detailQ.isLoading}
        canMutate={canMutate}
        onAction={onAction}
        onIpActionTaken={() => {
          void detailQ.refetch();
          void lifecycleQ.refetch();
        }}
      />

      {safeType === 'ip' && (
        <IPBehaviorSummaryPanel
          ip={id}
          profile={ipBehaviorProfileQ.data}
          findings={ipBehaviorFindings}
          loading={ipBehaviorProfileQ.isLoading || ipBehaviorFindingsQ.isLoading}
          error={ipBehaviorProfileQ.error || ipBehaviorFindingsQ.error}
        />
      )}

      {safeType === 'ip' && (
        <IPBehaviorRecommendationPanel
          ip={id}
          profile={ipBehaviorProfileQ.data}
          findings={ipBehaviorFindings}
          enrichment={ipEnrichQ.data}
        />
      )}

      <DashboardGrid>
        <DashboardGridItem span={{ base: 12, lg: 8 }}>
          <Panel padding="md" eyebrow="LIFECYCLE" title="Timeline" toneAccent="brand">
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList>
                {safeType === 'ip' && (
                  <TabsTrigger value="connections">Connections</TabsTrigger>
                )}
                <TabsTrigger value="timeline">Timeline</TabsTrigger>
                <TabsTrigger value="raw">Raw events</TabsTrigger>
              </TabsList>
              {safeType === 'ip' && (
                <TabsContent value="connections">
                  <IpLifecyclePanel ip={id} />
                </TabsContent>
              )}
              <TabsContent value="timeline">
                {safeType === 'ip' && ipTimelineQ.error ? (
                  <Alert variant="warning" className="mb-3">
                    {ipTimelineErrorMessage(ipTimelineQ.error)}
                  </Alert>
                ) : null}
                <InvestigateTimeline
                  items={lifecycleItems}
                  loading={lifecycleLoading}
                  hasMore={!!lifecycleQ.data?.next_cursor}
                  onLoadMore={() => setCursor(lifecycleQ.data?.next_cursor)}
                />
              </TabsContent>
              <TabsContent value="raw">
                {safeType === 'ip' && ipTimelineQ.error ? (
                  <Alert variant="warning" className="mb-3">
                    {ipTimelineErrorMessage(ipTimelineQ.error)}
                  </Alert>
                ) : null}
                {lifecycleItems.length === 0 ? (
                  <EmptyState title="No raw events" />
                ) : (
                  <RawEventsTable items={lifecycleItems} />
                )}
              </TabsContent>
            </Tabs>
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 4 }} className="flex flex-col gap-5">
          {safeType === 'ip' && (
            <IpEnrichmentCard enrichment={ipEnrichQ.data} loading={ipEnrichQ.isLoading} />
          )}
          <RelatedEntities related={relatedQ.data} loading={relatedQ.isLoading} />
        </DashboardGridItem>
      </DashboardGrid>
    </div>
  );
}

function detailWithLifecycleEventCount(detail: EntityDetailData | undefined, items: LifecycleItem[]): EntityDetailData | undefined {
  if (!detail) return detail;
  const eventCount = items.filter((item) => item.source === 'event' || item.source === 'events' || item.source === 'telemetry').length;
  if (eventCount <= (detail.counts?.events ?? 0)) return detail;
  return {
    ...detail,
    counts: {
      events: eventCount,
      alerts: detail.counts?.alerts ?? 0,
      audit: detail.counts?.audit ?? 0,
      sessions: detail.counts?.sessions ?? 0,
      remediations: detail.counts?.remediations ?? 0,
    },
  };
}

export function investigationTimelineItemToLifecycle(item: InvestigationTimelineItem): LifecycleItem {
  const peer = connectionPeerSummary(item);
  return {
    ts: item.ts,
    source: item.source_table === 'process_connections' ? 'event' : item.collector || item.source_table || 'event',
    severity: item.severity || 'info',
    actor: item.user_name || item.process_name,
    target: peer,
    summary: item.message || [item.event_type, peer].filter(Boolean).join(' ') || item.event_id || 'Timeline event',
    raw_id: item.source_record_id || item.event_id || item.raw_ref,
    metadata: {
      source_table: item.source_table,
      event_type: item.event_type,
      conn_id: item.conn_id,
      node_id: item.node_id,
      correlation_id: item.correlation_id,
      collector: item.collector,
      parser: item.parser,
      src_ip: item.src_ip,
      dst_ip: item.dst_ip,
      dst_port: item.dst_port,
      bytes_in: item.bytes_in,
      bytes_out: item.bytes_out,
      raw_ref: item.raw_ref,
    },
  };
}

export function mergeLifecycleItems(...groups: LifecycleItem[][]): LifecycleItem[] {
  const byKey = new Map<string, LifecycleItem>();
  for (const group of groups) {
    for (const item of group) {
      byKey.set(lifecycleItemKey(item), item);
    }
  }
  return Array.from(byKey.values()).sort((a, b) => {
    const delta = new Date(b.ts).getTime() - new Date(a.ts).getTime();
    if (Number.isFinite(delta) && delta !== 0) return delta;
    return (b.raw_id ?? b.summary).localeCompare(a.raw_id ?? a.summary);
  });
}

function lifecycleItemKey(item: LifecycleItem): string {
  if (item.raw_id) return `${item.raw_id}:${item.ts}`;
  return `${item.source}:${item.ts}:${item.summary}`;
}

function connectionPeerSummary(item: InvestigationTimelineItem): string | undefined {
  if (!item.src_ip && !item.dst_ip) return undefined;
  const dst = item.dst_ip ? `${item.dst_ip}${item.dst_port ? `:${item.dst_port}` : ''}` : '';
  return [item.src_ip, dst].filter(Boolean).join(' -> ');
}

function ipTimelineErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) {
    return `Connection timeline unavailable: ${error.message}`;
  }
  return 'Connection timeline unavailable.';
}

function RawEventsTable({ items }: { items: LifecycleItem[] }) {
  return (
    <div className="overflow-x-auto rounded-md border border-border-subtle bg-surface">
      <table className="min-w-full divide-y divide-border-subtle text-left text-xs">
        <thead className="bg-surface-2 text-[0.65rem] uppercase text-text-muted">
          <tr>
            <th className="whitespace-nowrap px-3 py-2 font-medium">Time</th>
            <th className="whitespace-nowrap px-3 py-2 font-medium">Source</th>
            <th className="whitespace-nowrap px-3 py-2 font-medium">Severity</th>
            <th className="px-3 py-2 font-medium">Summary</th>
            <th className="px-3 py-2 font-medium">Evidence</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border-subtle">
          {items.map((item, index) => (
            <tr key={`${item.raw_id ?? item.ts}-${index}`} className="align-top">
              <td className="whitespace-nowrap px-3 py-2 font-mono text-text-secondary">{formatTs(item.ts)}</td>
              <td className="whitespace-nowrap px-3 py-2">
                <StatusTag tone={sourceTone(item.source)}>{item.source}</StatusTag>
              </td>
              <td className="whitespace-nowrap px-3 py-2">
                {item.severity ? <StatusTag tone={severityTone(item.severity)}>{item.severity}</StatusTag> : <span className="text-text-muted">-</span>}
              </td>
              <td className="max-w-[28rem] px-3 py-2">
                <div className="line-clamp-2 text-text-primary">{item.summary || item.raw_id || '-'}</div>
                {(item.actor || item.target) && (
                  <div className="mt-1 truncate font-mono text-[0.68rem] text-text-muted">
                    {[item.actor, item.target].filter(Boolean).join(' -> ')}
                  </div>
                )}
              </td>
              <td className="max-w-[24rem] px-3 py-2">
                <div className="truncate font-mono text-[0.68rem] text-text-secondary">{rawEvidenceText(item)}</div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function rawEvidenceText(item: LifecycleItem): string {
  const pairs = metadataPairs(item.metadata);
  const prefix = item.raw_id ? [`id=${item.raw_id}`] : [];
  const out = [...prefix, ...pairs];
  return out.length > 0 ? out.join('  ') : '-';
}

function metadataPairs(metadata?: Record<string, unknown>): string[] {
  if (!metadata) return [];
  return Object.entries(metadata)
    .filter(([, value]) => ['string', 'number', 'boolean'].includes(typeof value))
    .slice(0, 5)
    .map(([key, value]) => `${key}=${String(value)}`);
}

function sourceTone(source: string): StateTone {
  switch (source) {
    case 'alert':
      return 'degraded';
    case 'audit':
    case 'event':
    case 'events':
    case 'telemetry':
      return 'info';
    case 'session':
      return 'healthy';
    case 'action':
    case 'remediation':
      return 'warning';
    default:
      return 'unknown';
  }
}

export function IPBehaviorSummaryPanel({
  ip,
  profile,
  findings,
  loading,
  error,
}: {
  ip: string;
  profile?: IPBehaviorIPProfile;
  findings: BehavioralAnomaly[];
  loading: boolean;
  error: unknown;
}) {
  const topFinding = topBehaviorFinding(findings);
  const topFindingPresentation = topFinding
    ? describeIPBehaviorFinding(topFinding, { countryLabel: profile?.countries?.[0] ?? topFinding.country_code, maxSignals: 4 })
    : null;
  const confidence = topFinding ? ipBehaviorConfidence(topFinding) : 0;
  const statusCounts = profile?.status_counts ?? {};
  const serverErrors = statusCountWithAggregate(statusCounts, ['500', '502', '503'], '5xx');
  const authFailures = statusCount(statusCounts, '401', '403');
  const topPaths = topFinding ? evidenceTopPaths(topFinding.evidence) : [];

  return (
    <Panel
      padding="md"
      eyebrow="IP BEHAVIOR"
      title="Attack behavior and exposure evidence"
      toneAccent={confidence >= 85 ? 'critical' : confidence >= 70 ? 'warning' : 'brand'}
    >
      {loading ? (
        <p className="text-sm text-text-muted">Loading behavior evidence...</p>
      ) : error ? (
        <EmptyState title="Behavior evidence unavailable" description={error instanceof Error ? error.message : 'The IP behavior APIs did not return data.'} />
      ) : !profile && findings.length === 0 ? (
        <EmptyState title="No behavior evidence" description={`No web, anomaly, or confidence records are available for ${ip}.`} />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
            <KpiTile label="Confidence" value={confidence ? `${confidence}%` : '0%'} tone={confidenceTone(confidence)} />
            <KpiTile label="Requests" value={(profile?.request_count ?? 0).toLocaleString()} tone="info" />
            <KpiTile label="Server errors" value={serverErrors.toLocaleString()} tone={serverErrors > 0 ? 'critical' : 'healthy'} />
            <KpiTile label="Auth failures" value={authFailures.toLocaleString()} tone={authFailures > 0 ? 'warning' : 'healthy'} />
            <KpiTile label="Bytes out" value={formatBytes(profile?.bytes_out ?? 0)} tone="accent" />
          </div>

          {topFindingPresentation ? (
            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <div className="flex flex-wrap items-center gap-2">
                <StatusTag tone={confidenceTone(confidence)}>{topFindingPresentation.categoryLabel}</StatusTag>
                <StatusTag tone={confidence >= 100 ? 'critical' : 'warning'}>{confidence}% confidence</StatusTag>
                {confidence >= 100 ? <StatusTag tone="critical">Auto-alerted at 100%</StatusTag> : null}
                {topFinding?.severity ? <StatusTag tone={severityTone(topFinding.severity)}>{topFinding.severity}</StatusTag> : null}
              </div>
              <p className="mt-2 text-sm leading-6 text-text-secondary">{topFindingPresentation.summary}</p>
              {topFindingPresentation.signals.length > 0 ? (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {topFindingPresentation.signals.slice(0, 6).map((signal) => (
                    <StatusTag key={signal} tone="info">{signal}</StatusTag>
                  ))}
                </div>
              ) : null}
            </div>
          ) : null}

          <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
            <EvidenceBlock label="Country / ASN" value={[profile?.countries?.join(', '), profile?.asns?.join(', ')].filter(Boolean).join(' / ') || 'Unknown'} />
            <EvidenceBlock label="App / group" value={[profile?.apps?.join(', '), profile?.server_groups?.join(', ')].filter(Boolean).join(' / ') || 'Unknown'} />
            <EvidenceBlock label="Observed" value={formatObservedRange(profile)} />
          </div>

          {topPaths.length > 0 ? (
            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <p className="mb-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Top probed paths</p>
              <div className="flex flex-wrap gap-1.5">
                {topPaths.map((path) => (
                  <StatusTag key={`${path.path}:${path.count}`} tone="warning">
                    {path.path} {path.count > 1 ? `x${path.count}` : ''}
                  </StatusTag>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      )}
    </Panel>
  );
}

export function IPBehaviorRecommendationPanel({
  ip,
  profile,
  findings,
  enrichment,
}: {
  ip: string;
  profile?: IPBehaviorIPProfile;
  findings: BehavioralAnomaly[];
  enrichment?: IpEnrichment;
}) {
  const topFinding = topBehaviorFinding(findings);
  const confidence = topFinding ? ipBehaviorConfidence(topFinding) : 0;
  const presentation = topFinding
    ? describeIPBehaviorFinding(topFinding, { countryLabel: profile?.countries?.[0] ?? topFinding.country_code, maxSignals: 4 })
    : null;
  const threatFeeds = enrichment?.threat_feeds ?? [];
  const listed = threatFeeds.length > 0;
  const serverErrors = statusCountWithAggregate(profile?.status_counts ?? {}, ['500', '502', '503'], '5xx');
  const authFailures = statusCount(profile?.status_counts ?? {}, '401', '403');
  const bytesOut = profile?.bytes_out ?? 0;
  const shouldShow = confidence >= 85 || listed || serverErrors > 0 || authFailures > 0 || bytesOut >= 10 * 1024 * 1024;
  if (!shouldShow) return null;

  const plan = ipBehaviorResponsePlan(presentation?.category ?? topFinding?.metric ?? 'ip_behavior', confidence, listed);
  const scopeParts = [
    profile?.node_ids?.length ? `${profile.node_ids.length} node${profile.node_ids.length === 1 ? '' : 's'}` : '',
    profile?.apps?.slice(0, 2).join(', '),
    profile?.server_groups?.slice(0, 2).join(', '),
  ].filter(Boolean);
  const scope = scopeParts.join(' / ') || 'affected scope';

  return (
    <Panel
      padding="md"
      eyebrow="SMART RESPONSE"
      title="Recommended response for this IP"
      toneAccent={confidence >= 100 || listed ? 'critical' : 'warning'}
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/control-room/exposure">
            Exposure
            <ArrowRight />
          </Link>
        </Button>
      }
    >
      <div className="grid gap-4 lg:grid-cols-[1fr_18rem]">
        <div className="space-y-3">
          <div className="rounded-lg border border-state-critical/25 bg-state-critical/5 p-3">
            <div className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 text-state-critical" />
              <div>
                <div className="flex flex-wrap items-center gap-1.5">
                  <StatusTag tone={confidenceTone(confidence)}>{confidence}% confidence</StatusTag>
                  {confidence >= 100 ? <StatusTag tone="critical">Auto-alert threshold</StatusTag> : null}
                  {enrichment ? (
                    <StatusTag tone={listed ? 'critical' : 'healthy'}>
                      {listed ? `Local blacklist: ${threatFeeds.map((feed) => feed.feed).slice(0, 2).join(', ')}` : 'Local blacklist: no hit'}
                    </StatusTag>
                  ) : null}
                  <StatusTag tone="info">{scope}</StatusTag>
                </div>
                <p className="mt-2 text-sm font-medium text-foreground">{plan.headline}</p>
                <p className="mt-1 text-sm text-text-secondary">{plan.summary}</p>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
            <RecommendationMetric label="Observed" value={formatObservedRange(profile)} />
            <RecommendationMetric label="Traffic" value={`${(profile?.request_count ?? 0).toLocaleString()} req / ${formatBytes(bytesOut)} out`} />
            <RecommendationMetric label="Failure signal" value={`${authFailures.toLocaleString()} auth / ${serverErrors.toLocaleString()} server`} />
          </div>

          <div className="rounded-lg border border-border-subtle bg-surface p-3">
            <p className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
              <ShieldCheck className="h-4 w-4 text-brand-400" />
              Resolution playbook
            </p>
            <ol className="space-y-2">
              {plan.steps.map((step, index) => (
                <li key={step} className="flex gap-2 text-sm text-text-secondary">
                  <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/15 font-mono text-[0.7rem] text-brand-400">
                    {index + 1}
                  </span>
                  <span>{step}</span>
                </li>
              ))}
            </ol>
          </div>
        </div>

        <div className="space-y-3">
          <div className="rounded-lg border border-border-subtle bg-surface p-3">
            <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Posture-template target</p>
            <p className="mt-1 text-sm font-medium text-foreground">{plan.posture}</p>
            <p className="mt-1 text-xs text-text-secondary">{plan.postureDetail}</p>
          </div>
          <div className="rounded-lg border border-border-subtle bg-surface p-3">
            <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Response governance</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              <StatusTag tone="warning">Approval gate</StatusTag>
              <StatusTag tone="info">Policy scoped</StatusTag>
              <StatusTag tone="info">Receipt required</StatusTag>
            </div>
            <p className="mt-2 text-xs text-text-secondary">
              Containment for {ip} should resolve through posture policy, approval state, enforcement receipts, and rollback evidence before it is treated as remediated.
            </p>
          </div>
          <div className="grid grid-cols-1 gap-2">
            <Button asChild variant="outline" size="sm" className="justify-between">
              <Link to="/security/network?tab=blocks">
                Active blocks
                <ArrowRight />
              </Link>
            </Button>
            <Button asChild variant="outline" size="sm" className="justify-between">
              <Link to="/security/webservers">
                Webserver controls
                <ArrowRight />
              </Link>
            </Button>
            <Button asChild variant="ghost" size="sm" className="justify-between">
              <Link to="/audit">
                Audit evidence
                <ArrowRight />
              </Link>
            </Button>
          </div>
        </div>
      </div>
    </Panel>
  );
}

function RecommendationMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
      <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{label}</p>
      <p className="mt-1 text-sm text-foreground">{value}</p>
    </div>
  );
}

function formatObservedRange(profile?: Pick<IPBehaviorIPProfile, 'first_seen_at' | 'last_seen_at'>): string {
  const firstSeen = meaningfulTimestamp(profile?.first_seen_at);
  const lastSeen = meaningfulTimestamp(profile?.last_seen_at);
  if (firstSeen && lastSeen) return `${firstSeen} - ${lastSeen}`;
  if (firstSeen) return `Since ${firstSeen}`;
  if (lastSeen) return `Until ${lastSeen}`;
  return 'No observations';
}

function meaningfulTimestamp(value?: string): string | null {
  if (!value) return null;
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return null;
  if (parsed.getUTCFullYear() < 2000) return null;
  return formatTs(value);
}

function ipBehaviorResponsePlan(category: string, confidence: number, listed: boolean): {
  headline: string;
  summary: string;
  steps: string[];
  posture: string;
  postureDetail: string;
} {
  const normalized = category.toLowerCase();
  if (normalized.includes('exfil')) {
    return {
      headline: 'Contain egress first, then prove destination legitimacy.',
      summary: 'Large outbound movement from a suspicious IP should become an egress decision, not only an alert review.',
      steps: [
        'Move affected nodes to update-only or full lockdown if transfer continues.',
        'Allow only Control One, DNS/NTP, package repositories, and approved application APIs while reviewing destinations.',
        'Resolve only after bytes-out, destination class, audit, and drift evidence are clean.',
      ],
      posture: 'Aggressive egress lockdown with TTL',
      postureDetail: 'Template target: egress default deny, explicit update/API allowlist, canary/rollback, and drift failure if the firewall backend cannot enforce it.',
    };
  }
  if (normalized.includes('credential')) {
    return {
      headline: 'Block the source and verify no successful session followed.',
      summary: 'Credential attacks need both IP containment and access evidence before the alert is safe to close.',
      steps: [
        'Block the IP on affected nodes; use fleet-wide block only if the source is active across groups.',
        'Review 401/403 volume, successful sessions after the attack window, and privileged role changes.',
        'Rotate exposed credentials and tighten management ingress when the same path or account repeats.',
      ],
      posture: 'Moderate ingress lockdown',
      postureDetail: 'Template target: protected management paths, inbound anomaly auto-block TTL, and audit-backed access review.',
    };
  }
  if (normalized.includes('exploit') || normalized.includes('scanner') || normalized.includes('probe')) {
    return {
      headline: 'Protect the public listener before tuning detections.',
      summary: 'Exploit and scanner confidence should drive default-deny or webserver enforcement on the exposed app.',
      steps: [
        listed ? 'Treat the local blacklist hit as enough evidence for immediate block on affected nodes.' : 'Block the source if paths, status diversity, or errors match the alert evidence.',
        'Review probed paths and enable webserver capture/enforce or default-deny firewall protection.',
        'Patch or close exposed paths, then verify no repeated 4xx/5xx spikes after containment.',
      ],
      posture: 'Aggressive ingress protection',
      postureDetail: 'Template target: ingress default deny, allowed service ports/sources, webserver enforcement receipts, and drift per node.',
    };
  }
  return {
    headline: confidence >= 100 || listed ? 'Treat this IP as action-ready until disproven.' : 'Review the anomaly and apply the narrowest safe control.',
    summary: 'The response should combine source containment, posture scope, and evidence required for closure.',
    steps: [
      'Inspect lifecycle, blacklist enrichment, app, node, country/ASN, and observed paths.',
      'Apply block/isolation only to the smallest scope that stops the behavior, then watch Active Blocks and audit.',
      'Close only after the owner decision and drift/remediation evidence are visible.',
    ],
    posture: confidence >= 100 || listed ? 'Time-boxed containment' : 'Observe with escalation threshold',
    postureDetail: 'Template target: resolved posture by node/group/fleet, dry-run impact preview, TTL override, and rollback.',
  };
}

function topBehaviorFinding(findings: BehavioralAnomaly[]): BehavioralAnomaly | undefined {
  return [...findings].sort((a, b) => ipBehaviorConfidence(b) - ipBehaviorConfidence(a))[0];
}

function EvidenceBlock({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
      <div className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{label}</div>
      <div className="mt-1 break-words text-sm text-foreground">{value}</div>
    </div>
  );
}

function statusCount(statusCounts: Record<string, number>, ...keys: string[]): number {
  return keys.reduce((sum, key) => sum + (statusCounts[key] ?? 0), 0);
}

function statusCountWithAggregate(statusCounts: Record<string, number>, exactKeys: string[], aggregateKey: string): number {
  const exact = statusCount(statusCounts, ...exactKeys);
  return exact > 0 ? exact : statusCounts[aggregateKey] ?? 0;
}

function confidenceTone(score: number): StateTone {
  if (score >= 85) return 'critical';
  if (score >= 70) return 'warning';
  if (score > 0) return 'info';
  return 'healthy';
}

function severityTone(severity?: string): StateTone {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    default:
      return 'unknown';
  }
}

function evidenceTopPaths(evidence?: Record<string, unknown>): Array<{ path: string; count: number }> {
  const raw = evidence?.top_paths;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((item) => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      return {
        path: typeof row.path === 'string' ? row.path : '',
        count: typeof row.count === 'number' ? row.count : 0,
      };
    })
    .filter((item): item is { path: string; count: number } => !!item?.path)
    .slice(0, 10);
}
