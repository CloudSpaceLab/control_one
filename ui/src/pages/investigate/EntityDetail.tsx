import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { useEffect, useMemo, useState } from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Panel, SectionHeader, EmptyState, KpiTile, StatusTag, type StateTone } from '@/components/kit';
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
        detail={detailQ.data}
        loading={detailQ.isLoading}
        canMutate={canMutate}
        onAction={onAction}
      />

      {safeType === 'ip' && (
        <IPBehaviorSummaryPanel
          ip={id}
          profile={ipBehaviorProfileQ.data}
          findings={ipBehaviorFindingsQ.data?.data ?? []}
          loading={ipBehaviorProfileQ.isLoading || ipBehaviorFindingsQ.isLoading}
          error={ipBehaviorProfileQ.error || ipBehaviorFindingsQ.error}
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
                <InvestigateTimeline
                  items={accumulated}
                  loading={lifecycleQ.isLoading}
                  hasMore={!!lifecycleQ.data?.next_cursor}
                  onLoadMore={() => setCursor(lifecycleQ.data?.next_cursor)}
                />
              </TabsContent>
              <TabsContent value="raw">
                {accumulated.length === 0 ? (
                  <EmptyState title="No raw events" />
                ) : (
                  <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface-2 p-3 font-mono text-[0.7rem] leading-relaxed text-text-secondary">
                    {JSON.stringify(accumulated, null, 2)}
                  </pre>
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

function IPBehaviorSummaryPanel({
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
  const topFinding = [...findings].sort((a, b) => ipBehaviorConfidence(b) - ipBehaviorConfidence(a))[0];
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
            <EvidenceBlock label="Observed" value={`${formatTs(profile?.first_seen_at)} - ${formatTs(profile?.last_seen_at)}`} />
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
