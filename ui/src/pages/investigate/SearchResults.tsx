import { useQuery } from '@tanstack/react-query';
import { Bookmark, Search } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { EmptyState, Eyebrow, Panel, SectionHeader, StatusTag, type StateTone } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { entityRoute, ENTITY_TYPE_LABELS } from '@/lib/entity';
import type { EntityType } from '@/components/kit';
import type { ClassificationChip, InvestigateSearchResult } from '@/lib/api';

const SEV_TO_TONE: Record<string, StateTone> = {
  critical: 'critical',
  high: 'degraded',
  warning: 'warning',
  info: 'info',
  healthy: 'healthy',
  unknown: 'unknown',
};

export function SearchResults(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const q = params.get('q') ?? '';
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [tab, setTab] = useState('all');
  const [refineQuery, setRefineQuery] = useState(q);

  // Keep refine input in sync when URL changes (e.g. browser back)
  useEffect(() => {
    setRefineQuery(q);
  }, [q]);

  const handleRefine = () => {
    const trimmed = refineQuery.trim();
    if (trimmed && trimmed !== q) {
      setParams({ q: trimmed });
      setTab('all');
    }
  };

  const searchQ = useQuery<InvestigateSearchResult>({
    queryKey: ['search', currentTenantId, q],
    queryFn: () => client.investigateSearch({ tenantId: currentTenantId, q, limit: 200 }),
    enabled: q.length > 0 && !!currentTenantId,
  });

  const items = searchQ.data?.items ?? [];
  const facets = searchQ.data?.facets ?? [];

  const filtered = tab === 'all' ? items : items.filter((i) => i.type === tab);

  return (
    <div className="flex flex-col gap-5">
      {/* Refinement search bar */}
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" />
          <Input
            className="pl-9"
            value={refineQuery}
            onChange={(e) => setRefineQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleRefine()}
            placeholder="Refine search…"
          />
        </div>
        <Button variant="secondary" onClick={handleRefine}>
          Search
        </Button>
      </div>

      <SectionHeader
        eyebrow="SEARCH RESULTS"
        title={
          <span className="inline-flex items-center gap-3">
            <span className="font-mono text-text-muted">›</span>
            <span className="break-all">{q || '(empty query)'}</span>
          </span>
        }
        description={
          searchQ.isLoading
            ? 'Searching across events, alerts, audit and tags…'
            : `${items.length.toLocaleString()} match${items.length === 1 ? '' : 'es'}`
        }
        actions={
          <Button variant="secondary" size="md">
            <Bookmark className="h-4 w-4" /> Save search
          </Button>
        }
      />

      {q.length === 0 ? (
        <EmptyState title="Enter a query" description="Type in the search bar above and press Enter or click Search." />
      ) : (
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="all">All ({items.length})</TabsTrigger>
            {facets.map((f) => (
              <TabsTrigger key={f.type} value={f.type}>
                {ENTITY_TYPE_LABELS[f.type as EntityType] ?? f.type} ({f.count})
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value={tab}>
            <Panel padding="sm" tone="inset">
              {searchQ.isLoading ? (
                <p className="px-3 py-6 text-center text-sm text-text-muted">Loading…</p>
              ) : filtered.length === 0 ? (
                <EmptyState
                  title="No results"
                  description="Try a different query or remove the type filter."
                />
              ) : (
                <ul className="flex flex-col">
                  {filtered.map((hit, idx) => (
                    <li
                      key={`${hit.type}-${hit.id}-${idx}`}
                      className="border-b border-border-subtle last:border-0"
                    >
                      <Link
                        to={entityRoute(hit.type as EntityType, hit.id)}
                        className="flex items-start gap-3 px-3 py-3 transition-colors hover:bg-hover"
                      >
                        <Eyebrow>{ENTITY_TYPE_LABELS[hit.type as EntityType] ?? hit.type}</Eyebrow>
                        <div className="flex min-w-0 flex-1 flex-col gap-1">
                          <span className="font-mono text-sm text-foreground break-all">{hit.id}</span>
                          {hit.snippet && (
                            <span className="line-clamp-2 text-xs text-text-secondary">{hit.snippet}</span>
                          )}
                          {hit.classification && hit.classification.length > 0 && (
                            <div className="flex flex-wrap gap-1.5">
                              {hit.classification.map((c: ClassificationChip, i: number) => (
                                <StatusTag key={i} tone={SEV_TO_TONE[c.tone ?? 'info'] ?? 'info'}>
                                  {c.label}
                                </StatusTag>
                              ))}
                            </div>
                          )}
                        </div>
                        <span className="font-mono text-[0.65rem] tabular-nums text-text-muted">
                          {hit.score.toFixed(2)}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </Panel>
          </TabsContent>
        </Tabs>
      )}
    </div>
  );
}
