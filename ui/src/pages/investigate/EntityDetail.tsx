import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Panel, SectionHeader, EmptyState } from '@/components/kit';
import { DashboardGrid, DashboardGridItem } from '@/components/shell';
import {
  EntityHeader,
  InvestigateTimeline,
  IpEnrichmentCard,
  RelatedEntities,
} from '@/components/investigate';
import { useApiClient } from '@/hooks/useApiClient';
import { useRolePick } from '@/hooks/useRolePick';
import { toast } from 'sonner';
import { ENTITY_TYPE_LABELS } from '@/lib/entity';
import type { EntityType } from '@/components/kit';
import type {
  EntityDetail as EntityDetailData,
  EntityLifecycle,
  EntityRelated,
  IpEnrichment,
  LifecycleItem,
} from '@/lib/api';

const VALID_TYPES: EntityType[] = [
  'ip', 'process', 'file', 'hash', 'user', 'host', 'domain', 'url', 'session', 'alert', 'rule', 'tenant',
];

export function EntityDetail(): JSX.Element {
  const { type, id } = useParams<{ type: string; id: string }>();
  const client = useApiClient();
  const { isAdmin, isOperator } = useRolePick();
  const canMutate = isAdmin || isOperator;
  const [tab, setTab] = useState('timeline');
  const [cursor, setCursor] = useState<string | undefined>();
  const [accumulated, setAccumulated] = useState<LifecycleItem[]>([]);

  const safeType = (VALID_TYPES.includes(type as EntityType) ? type : 'ip') as EntityType;

  const detailQ = useQuery<EntityDetailData>({
    queryKey: ['entity', safeType, id],
    queryFn: () => client.getEntity(safeType, id ?? ''),
    enabled: !!id,
  });

  const lifecycleQ = useQuery<EntityLifecycle>({
    queryKey: ['entity.lifecycle', safeType, id, cursor],
    queryFn: () =>
      client.getEntityLifecycle(safeType, id ?? '', { cursor, limit: 50 }),
    enabled: !!id,
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
    queryKey: ['entity.related', safeType, id],
    queryFn: () => client.getEntityRelated(safeType, id ?? ''),
    enabled: !!id,
  });

  const ipEnrichQ = useQuery<IpEnrichment>({
    queryKey: ['entity.ip.enrich', id],
    queryFn: () => client.enrichIp(id ?? ''),
    enabled: !!id && safeType === 'ip',
  });

  const onAction = async (action: 'block' | 'allow' | 'quarantine') => {
    try {
      await client.entityAction(safeType, id ?? '', { action });
      toast.success(`${action} action queued`);
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

      <DashboardGrid>
        <DashboardGridItem span={{ base: 12, lg: 8 }}>
          <Panel padding="md" eyebrow="LIFECYCLE" title="Timeline" toneAccent="brand">
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList>
                <TabsTrigger value="timeline">Timeline</TabsTrigger>
                <TabsTrigger value="raw">Raw events</TabsTrigger>
              </TabsList>
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
