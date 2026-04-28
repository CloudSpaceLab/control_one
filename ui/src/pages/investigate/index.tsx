import { useQuery } from '@tanstack/react-query';
import { Bookmark, Hash, Network, Search, Terminal, User as UserIcon } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Eyebrow, Panel, SectionHeader } from '@/components/kit';
import { GlobalSearch } from '@/components/shell';
import { useApiClient } from '@/hooks/useApiClient';
import { useLocalStorage } from '@/hooks/useLocalStorage';
import { entityRoute } from '@/lib/entity';
import { classifyValue } from '@/lib/entity';
import type { SavedSearch } from '@/lib/api';
import type { EntityType } from '@/components/kit';

export function InvestigateHome(): JSX.Element {
  const client = useApiClient();
  const [recents] = useLocalStorage<string[]>('co.search.recents', []);

  const savedQ = useQuery({
    queryKey: ['saved-searches'],
    queryFn: () => client.listSavedSearches(),
  });

  const examples: { type: EntityType; value: string; label: string }[] = [
    { type: 'ip', value: '8.8.8.8', label: 'External DNS' },
    { type: 'ip', value: '10.0.0.1', label: 'Internal gateway' },
    { type: 'hash', value: 'd41d8cd98f00b204e9800998ecf8427e', label: 'Empty MD5' },
    { type: 'process', value: 'sshd', label: 'sshd process' },
    { type: 'user', value: 'admin@example.com', label: 'Admin user' },
  ];

  return (
    <div className="flex flex-col gap-6">
      <SectionHeader
        eyebrow="INVESTIGATE"
        title="Search & lifecycle"
        description="Search any IP, process, file, hash, user, or hostname. See its complete lifecycle across events, alerts, audit, sessions and remediations."
      />

      <div className="rounded-lg border border-border-subtle bg-elevated p-6 shadow-[var(--shadow-panel)]">
        <Eyebrow tone="brand">SEARCH</Eyebrow>
        <h2 className="mt-1 mb-4 font-display text-xl font-semibold text-foreground">
          What do you want to investigate?
        </h2>
        <GlobalSearch />
        <p className="mt-3 text-xs text-text-muted">
          Tip: paste an IP, SHA256, email, hostname or process name. Press <kbd className="rounded border border-border-subtle bg-surface px-1 font-mono text-[0.65rem]">Ctrl/⌘+K</kbd> from anywhere.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Panel
          padding="md"
          eyebrow="RECENT"
          title="Recent searches"
          actions={recents.length > 0 ? <span className="font-mono text-xs text-text-muted">{recents.length}</span> : null}
        >
          {recents.length === 0 ? (
            <p className="text-sm text-text-muted">No recent searches yet.</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {recents.slice(0, 10).map((r) => {
                const det = classifyValue(r);
                if (det.type === 'unknown') {
                  return (
                    <li key={r}>
                      <Link
                        to={`/search?q=${encodeURIComponent(r)}`}
                        className="flex items-center gap-2 rounded-md border border-transparent bg-surface px-3 py-2 transition-colors hover:border-border-strong hover:bg-hover"
                      >
                        <Search className="h-4 w-4 text-text-muted" />
                        <span className="font-mono text-sm text-foreground">{r}</span>
                      </Link>
                    </li>
                  );
                }
                const Icon = ENTITY_ICON[det.type as EntityType] ?? Search;
                return (
                  <li key={r}>
                    <Link
                      to={entityRoute(det.type as EntityType, r)}
                      className="flex items-center gap-2 rounded-md border border-transparent bg-surface px-3 py-2 transition-colors hover:border-border-strong hover:bg-hover"
                    >
                      <Icon className="h-4 w-4 text-brand-400" />
                      <span className="font-mono text-sm text-foreground">{r}</span>
                    </Link>
                  </li>
                );
              })}
            </ul>
          )}
        </Panel>

        <Panel
          padding="md"
          eyebrow="SAVED"
          title="Saved investigations"
          actions={
            <Button asChild variant="ghost" size="sm">
              <Link to="/investigate/saved">View all</Link>
            </Button>
          }
        >
          {savedQ.isLoading ? (
            <p className="text-sm text-text-muted">Loading…</p>
          ) : (savedQ.data?.items.length ?? 0) === 0 ? (
            <p className="text-sm text-text-muted">No saved searches. Save any investigation to come back to it later.</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {(savedQ.data?.items ?? []).slice(0, 8).map((s: SavedSearch) => (
                <li key={s.id}>
                  <Link
                    to={`/search?q=${encodeURIComponent(s.query)}`}
                    className="flex items-center gap-2 rounded-md border border-transparent bg-surface px-3 py-2 transition-colors hover:border-border-strong hover:bg-hover"
                  >
                    <Bookmark className="h-4 w-4 text-accent-400" />
                    <div className="flex min-w-0 flex-col">
                      <span className="text-sm text-foreground">{s.name}</span>
                      <span className="truncate font-mono text-[0.65rem] text-text-muted">{s.query}</span>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </div>

      <Panel padding="md" eyebrow="EXAMPLES" title="Try one of these">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {examples.map((ex) => {
            const Icon = ENTITY_ICON[ex.type] ?? Search;
            return (
              <Link
                key={`${ex.type}-${ex.value}`}
                to={entityRoute(ex.type, ex.value)}
                className="flex items-center gap-3 rounded-md border border-border-subtle bg-surface px-3 py-3 transition-colors hover:border-border-strong hover:bg-hover"
              >
                <Icon className="h-4 w-4 text-brand-400" />
                <div className="flex min-w-0 flex-col">
                  <span className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                    {ex.label}
                  </span>
                  <span className="truncate font-mono text-sm text-foreground">{ex.value}</span>
                </div>
              </Link>
            );
          })}
        </div>
      </Panel>
    </div>
  );
}

const ENTITY_ICON: Partial<Record<EntityType, typeof Search>> = {
  ip: Network,
  hash: Hash,
  process: Terminal,
  file: Terminal,
  user: UserIcon,
  host: Terminal,
  domain: Network,
  url: Network,
};
