import { Link } from 'react-router-dom';
import { Panel, EmptyState } from '@/components/kit';
import { Skeleton } from '@/components/ui/skeleton';
import type { EntityRelated } from '@/lib/api';
import { ENTITY_TYPE_LABELS, entityRoute } from '@/lib/entity';
import type { EntityType } from '@/components/kit';

export interface RelatedEntitiesProps {
  related?: EntityRelated;
  loading?: boolean;
}

export function RelatedEntities({ related, loading }: RelatedEntitiesProps) {
  return (
    <Panel padding="md" eyebrow="PIVOT" title="Related entities">
      {loading ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-10 w-full" />)}
        </div>
      ) : !related || related.related.length === 0 ? (
        <EmptyState title="No related entities yet" description="Co-occurring entities will surface here." />
      ) : (
        <ul className="flex flex-col gap-1">
          {related.related.map((r) => (
            <li key={`${r.type}-${r.id}`}>
              <Link
                to={entityRoute(r.type as EntityType, r.id)}
                className="flex items-center justify-between gap-3 rounded-md border border-transparent bg-surface px-3 py-2 transition-colors hover:border-border-strong hover:bg-hover"
              >
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
                    {ENTITY_TYPE_LABELS[r.type as EntityType] ?? r.type}
                  </span>
                  <span className="truncate font-mono text-sm text-foreground">{r.id}</span>
                </span>
                <span className="font-mono text-xs tabular-nums text-state-info">
                  {r.co_occurrences} co-occur
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
