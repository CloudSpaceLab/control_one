import { Activity, AlertTriangle, FileText, ShieldAlert, Terminal, Workflow } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { EmptyState, StatusDot, type StateTone } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import type { LifecycleItem } from '@/lib/api';

const SEVERITY_TONE: Record<string, StateTone> = {
  critical: 'critical',
  high: 'degraded',
  medium: 'warning',
  low: 'info',
  info: 'info',
};

const SOURCE_ICON: Record<string, JSX.Element> = {
  alert: <AlertTriangle className="h-4 w-4" />,
  audit: <FileText className="h-4 w-4" />,
  rule: <ShieldAlert className="h-4 w-4" />,
  session: <Terminal className="h-4 w-4" />,
  remediation: <Workflow className="h-4 w-4" />,
  event: <Activity className="h-4 w-4" />,
};

export interface InvestigateTimelineProps {
  items: LifecycleItem[];
  loading?: boolean;
  hasMore?: boolean;
  onLoadMore?: () => void;
}

export function InvestigateTimeline({ items, loading, hasMore, onLoadMore }: InvestigateTimelineProps) {
  const [active, setActive] = useState<LifecycleItem | null>(null);

  const grouped = useMemo(() => {
    const groups = new Map<string, LifecycleItem[]>();
    for (const i of items) {
      const day = (i.ts || '').slice(0, 10);
      if (!groups.has(day)) groups.set(day, []);
      groups.get(day)!.push(i);
    }
    return Array.from(groups.entries());
  }, [items]);

  if (loading && items.length === 0) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    );
  }

  if (!loading && items.length === 0) {
    return (
      <EmptyState
        icon={<Activity />}
        title="No lifecycle events"
        description="Once this entity shows up in events, alerts, audits or sessions, you'll see them here."
      />
    );
  }

  return (
    <>
      <div className="flex flex-col gap-5">
        {grouped.map(([day, dayItems]) => (
          <section key={day} className="flex flex-col gap-2">
            <div className="sticky top-0 z-[1] -mx-2 bg-surface/80 px-2 py-1 backdrop-blur">
              <span className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                {day}
              </span>
            </div>
            <ol className="relative ml-2 flex flex-col gap-1.5 border-l border-border-subtle pl-4">
              {dayItems.map((item, idx) => {
                const tone = SEVERITY_TONE[item.severity ?? ''] ?? 'info';
                return (
                  <li key={`${item.ts}-${idx}-${item.raw_id ?? ''}`} className="relative">
                    <span
                      aria-hidden
                      className="absolute -left-[22px] top-1.5"
                    >
                      <StatusDot tone={tone} size="sm" />
                    </span>
                    <button
                      type="button"
                      onClick={() => setActive(item)}
                      className={cn(
                        'group flex w-full items-start gap-3 rounded-md border border-transparent bg-surface px-3 py-2 text-left transition-colors',
                        'hover:border-border-strong hover:bg-hover',
                      )}
                    >
                      <span className="mt-0.5 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-surface-2 text-text-muted">
                        {SOURCE_ICON[item.source] ?? <Activity className="h-4 w-4" />}
                      </span>
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
                            {item.source} · {item.severity ?? 'info'}
                          </span>
                          <span className="font-mono text-[0.65rem] text-text-muted">
                            {new Date(item.ts).toLocaleTimeString()}
                          </span>
                        </div>
                        <p className="text-sm text-foreground">{item.summary}</p>
                        {(item.actor || item.target) && (
                          <p className="font-mono text-xs text-text-secondary">
                            {item.actor && <span>actor=<span className="text-text-primary">{item.actor}</span> </span>}
                            {item.target && <span>target=<span className="text-text-primary">{item.target}</span></span>}
                          </p>
                        )}
                      </div>
                    </button>
                  </li>
                );
              })}
            </ol>
          </section>
        ))}
        {hasMore && (
          <Button variant="ghost" onClick={onLoadMore} disabled={loading} className="self-center">
            {loading ? 'Loading…' : 'Load older events'}
          </Button>
        )}
      </div>

      <Sheet open={!!active} onOpenChange={(o) => !o && setActive(null)}>
        <SheetContent side="right" className="overflow-y-auto sm:max-w-xl">
          {active && (
            <>
              <SheetHeader>
                <SheetTitle className="font-mono text-base">
                  {active.source.toUpperCase()} · {active.severity ?? 'info'}
                </SheetTitle>
                <SheetDescription>{new Date(active.ts).toLocaleString()}</SheetDescription>
              </SheetHeader>
              <div className="mt-4 flex flex-col gap-3">
                <p className="text-sm text-foreground">{active.summary}</p>
                <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface-2 p-3 font-mono text-[0.75rem] leading-relaxed text-text-secondary">
                  {JSON.stringify(active, null, 2)}
                </pre>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}
