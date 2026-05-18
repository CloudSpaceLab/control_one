import {
  Activity,
  AlertTriangle,
  Building2,
  FileText,
  Hash,
  KeyRound,
  Network,
  Search,
  Server,
  ShieldAlert,
  Terminal,
  User as UserIcon,
} from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from '@/components/ui/command';
import { useLocalStorage } from '@/hooks/useLocalStorage';
import { classifyValue, ENTITY_TYPE_LABELS, entityRoute } from '@/lib/entity';
import { cn } from '@/lib/utils';
import type { EntityType } from '@/components/kit';

const NAV_ITEMS: { label: string; route: string; icon: ReactNode; group: string }[] = [
  { label: 'Control Room', route: '/', icon: <Activity />, group: 'Pages' },
  { label: 'Alerts', route: '/alerts', icon: <AlertTriangle />, group: 'Pages' },
  { label: 'Investigate', route: '/investigate', icon: <Search />, group: 'Pages' },
  { label: 'Servers', route: '/nodes', icon: <Server />, group: 'Pages' },
  { label: 'Network & exposure', route: '/security/network', icon: <Network />, group: 'Pages' },
  { label: 'Patch posture', route: '/infrastructure/patch', icon: <ShieldAlert />, group: 'Pages' },
  { label: 'Compliance', route: '/compliance', icon: <FileText />, group: 'Pages' },
  { label: 'Access', route: '/access', icon: <KeyRound />, group: 'Pages' },
  { label: 'Audit log', route: '/audit', icon: <FileText />, group: 'Pages' },
];

const QUICK_NAV_ROUTES = ['/', '/alerts', '/investigate', '/nodes', '/security/network', '/infrastructure/patch'];

// Discoverability prompts shown under the empty state.
const EXAMPLE_QUERIES = ['8.8.8.8', '139.162.40.237', 'admin@', 'sshd'];

const ENTITY_ICON: Record<EntityType, ReactNode> = {
  ip: <Network />,
  process: <Terminal />,
  file: <FileText />,
  hash: <Hash />,
  user: <UserIcon />,
  host: <Server />,
  domain: <Network />,
  url: <Network />,
  session: <Terminal />,
  alert: <AlertTriangle />,
  rule: <ShieldAlert />,
  tenant: <Building2 />,
};

export function GlobalSearch({ trigger }: { trigger?: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [recents, setRecents] = useLocalStorage<string[]>('co.search.recents', []);
  const navigate = useNavigate();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  const onPick = (path: string, persist?: string) => {
    if (persist) {
      setRecents((prev) => [persist, ...prev.filter((r) => r !== persist)].slice(0, 25));
    }
    setOpen(false);
    setQuery('');
    navigate(path);
  };

  const detection = classifyValue(query);
  const isMac = typeof navigator !== 'undefined' && /mac/i.test(navigator.platform);

  const triggerNode = trigger ? (
    <span onClick={() => setOpen(true)} role="button" tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setOpen(true); } }}>
      {trigger}
    </span>
  ) : (
    <button
      type="button"
      onClick={() => setOpen(true)}
      className="group relative flex h-9 w-full max-w-lg items-center gap-2.5 rounded-full border border-border-subtle bg-surface/60 px-3.5 text-sm text-text-muted shadow-sm transition-all hover:border-border-strong hover:bg-surface hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40"
      aria-label="Open search"
    >
      <Search className="h-3.5 w-3.5 shrink-0 text-text-muted group-hover:text-text-secondary transition-colors" />
      <span className="flex-1 text-left text-sm">Search anything…</span>
      <span className="hidden items-center gap-0.5 sm:flex">
        <kbd className="inline-flex h-5 items-center rounded border border-border-subtle bg-surface px-1 font-mono text-[0.6rem] text-text-muted">
          {isMac ? '⌘' : 'Ctrl'}
        </kbd>
        <kbd className="inline-flex h-5 items-center rounded border border-border-subtle bg-surface px-1 font-mono text-[0.6rem] text-text-muted">
          K
        </kbd>
      </span>
    </button>
  );

  return (
    <>
      {triggerNode}
      <CommandDialog open={open} onOpenChange={setOpen}>
        <CommandInput
          placeholder="Paste an IP, hash, hostname, or user"
          value={query}
          onValueChange={setQuery}
          onPaste={(e) => {
            const text = e.clipboardData.getData('text').trim();
            if (text) setQuery(text);
          }}
        />
        <CommandList>
          <CommandEmpty>
            {query.trim() ? (
              <div className="flex flex-col items-center gap-1 py-2">
                <span className="text-sm text-text-secondary">No results for <span className="font-mono text-foreground">&ldquo;{query}&rdquo;</span></span>
                <span className="text-xs text-text-muted">Press ↵ to search across all data</span>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-2 py-3">
                <Search className="h-8 w-8 text-text-muted opacity-40" />
                <span className="text-sm text-text-secondary">Paste an IP, hash, hostname, user…</span>
                <div className="flex flex-wrap items-center justify-center gap-1.5 pt-1">
                  <span className="text-[0.65rem] uppercase tracking-wider text-text-muted">Try</span>
                  {EXAMPLE_QUERIES.map((ex) => (
                    <button
                      key={ex}
                      type="button"
                      className="rounded-full border border-border-subtle bg-surface px-2 py-0.5 font-mono text-[0.65rem] text-text-secondary transition-colors hover:border-border-strong hover:bg-hover hover:text-foreground"
                      onClick={() => setQuery(ex)}
                    >
                      {ex}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </CommandEmpty>

          {detection.type === 'ip' && query.trim().length > 1 && (
            <>
              <CommandGroup heading="IP address detected">
                <CommandItem
                  value={`__ip-investigate-${query}`}
                  onSelect={() =>
                    onPick(entityRoute('ip', query.trim()), query.trim())
                  }
                  className="my-0.5 border border-brand-500/40 bg-brand-500/5"
                >
                  <span className="inline-flex h-7 w-7 items-center justify-center rounded-md bg-brand-500/15 text-brand-400 [&_svg]:h-4 [&_svg]:w-4">
                    {ENTITY_ICON.ip}
                  </span>
                  <div className="flex min-w-0 flex-col">
                    <span className="text-sm font-semibold text-foreground">
                      Investigate this IP across all nodes →
                    </span>
                    <span className="font-mono text-xs text-text-muted truncate">
                      {query.trim()} · all connection lifecycles · last 7d
                    </span>
                  </div>
                  <CommandShortcut>↵</CommandShortcut>
                </CommandItem>
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {detection.type !== 'unknown' && detection.type !== 'ip' && query.trim().length > 1 && (
            <>
              <CommandGroup heading="Investigate">
                <CommandItem
                  value={`__investigate-${query}`}
                  onSelect={() =>
                    onPick(entityRoute(detection.type as EntityType, query.trim()), query.trim())
                  }
                >
                  <span className={cn('inline-flex h-5 w-5 items-center justify-center text-brand-400')}>
                    {ENTITY_ICON[detection.type as EntityType]}
                  </span>
                  <div className="flex min-w-0 flex-col">
                    <span className="text-sm">
                      Investigate {ENTITY_TYPE_LABELS[detection.type as EntityType]}
                    </span>
                    <span className="font-mono text-xs text-text-muted truncate">{query.trim()}</span>
                  </div>
                  <CommandShortcut>↵</CommandShortcut>
                </CommandItem>
                <CommandItem
                  value={`__search-${query}`}
                  onSelect={() => onPick(`/search?q=${encodeURIComponent(query.trim())}`, query.trim())}
                >
                  <Search className="h-4 w-4 text-text-muted" />
                  <span className="text-sm">Full search results for &ldquo;{query.trim()}&rdquo;</span>
                  <CommandShortcut>↵ ⌥</CommandShortcut>
                </CommandItem>
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {recents.length > 0 && (
            <>
              <CommandGroup heading="Recent searches">
                <div className="flex items-center justify-between px-2 pb-1">
                  <span className="text-xs text-text-muted">{recents.length} recent{recents.length !== 1 ? 's' : ''}</span>
                  <button
                    type="button"
                    className="text-xs text-text-muted hover:text-foreground transition-colors"
                    onClick={(e) => { e.stopPropagation(); setRecents([]); }}
                  >
                    Clear
                  </button>
                </div>
                {recents.slice(0, 6).map((r) => {
                  const det = classifyValue(r);
                  return (
                    <CommandItem
                      key={r}
                      value={`__recent-${r}`}
                      onSelect={() => {
                        if (det.type !== 'unknown') onPick(entityRoute(det.type as EntityType, r), r);
                        else onPick(`/search?q=${encodeURIComponent(r)}`, r);
                      }}
                    >
                      <Search className="h-4 w-4 text-text-muted" />
                      <span className="font-mono text-xs">{r}</span>
                    </CommandItem>
                  );
                })}
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {!query.trim() && (
            <>
              <CommandGroup heading="Quick navigation">
                {NAV_ITEMS.filter(item => QUICK_NAV_ROUTES.includes(item.route)).map((item) => (
                  <CommandItem
                    key={item.route}
                    value={item.label}
                    onSelect={() => onPick(item.route)}
                  >
                    <span className="inline-flex h-5 w-5 items-center justify-center text-brand-400 [&_svg]:h-4 [&_svg]:w-4">
                      {item.icon}
                    </span>
                    <span>{item.label}</span>
                    <span className="ml-auto font-mono text-[0.65rem] text-text-muted">{item.route}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          <CommandGroup heading="Pages">
            {NAV_ITEMS.map((item) => (
              <CommandItem
                key={item.route}
                value={item.label}
                onSelect={() => onPick(item.route)}
              >
                <span className="inline-flex h-5 w-5 items-center justify-center text-text-muted [&_svg]:h-4 [&_svg]:w-4">
                  {item.icon}
                </span>
                <span>{item.label}</span>
                <span className="ml-auto font-mono text-[0.65rem] text-text-muted">{item.route}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </>
  );
}
