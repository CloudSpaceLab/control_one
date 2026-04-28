import {
  Activity,
  AlertTriangle,
  Boxes,
  Building2,
  FileText,
  Hash,
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
import { Button } from '@/components/ui/button';
import { useLocalStorage } from '@/hooks/useLocalStorage';
import { classifyValue, ENTITY_TYPE_LABELS, entityRoute } from '@/lib/entity';
import { cn } from '@/lib/utils';
import type { EntityType } from '@/components/kit';

const NAV_ITEMS: { label: string; route: string; icon: ReactNode; group: string }[] = [
  { label: 'Dashboard', route: '/', icon: <Activity />, group: 'Pages' },
  { label: 'Alerts', route: '/alerts', icon: <AlertTriangle />, group: 'Pages' },
  { label: 'Investigate', route: '/investigate', icon: <Search />, group: 'Pages' },
  { label: 'Saved searches', route: '/investigate/saved', icon: <FileText />, group: 'Pages' },
  { label: 'Rules', route: '/rules', icon: <ShieldAlert />, group: 'Pages' },
  { label: 'Threat feeds', route: '/threat-feeds', icon: <Network />, group: 'Pages' },
  { label: 'Compliance', route: '/compliance', icon: <FileText />, group: 'Pages' },
  { label: 'Audit log', route: '/audit', icon: <FileText />, group: 'Pages' },
  { label: 'Telemetry', route: '/telemetry', icon: <Activity />, group: 'Pages' },
  { label: 'Recommendations', route: '/recommendations', icon: <ShieldAlert />, group: 'Pages' },
  { label: 'Connections', route: '/connections', icon: <Network />, group: 'Pages' },
  { label: 'Sessions', route: '/sessions', icon: <Terminal />, group: 'Pages' },
  { label: 'Just-in-time access', route: '/access', icon: <UserIcon />, group: 'Pages' },
  { label: 'Nodes', route: '/nodes', icon: <Server />, group: 'Pages' },
  { label: 'Fleet enrol', route: '/fleet-enroll', icon: <Server />, group: 'Pages' },
  { label: 'Hypervisors', route: '/hypervisors', icon: <Boxes />, group: 'Pages' },
  { label: 'Templates', route: '/templates', icon: <FileText />, group: 'Pages' },
  { label: 'Jobs', route: '/jobs', icon: <Activity />, group: 'Pages' },
  { label: 'Tenants', route: '/tenants', icon: <Building2 />, group: 'Pages' },
  { label: 'Users', route: '/users', icon: <UserIcon />, group: 'Pages' },
  { label: 'Roles & permissions', route: '/roles', icon: <UserIcon />, group: 'Pages' },
  { label: 'Secrets', route: '/secrets', icon: <FileText />, group: 'Pages' },
  { label: 'Settings', route: '/settings', icon: <FileText />, group: 'Pages' },
];

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
    <Button
      variant="secondary"
      size="md"
      onClick={() => setOpen(true)}
      className="h-9 w-full max-w-md justify-between gap-3 text-text-secondary"
    >
      <span className="inline-flex items-center gap-2">
        <Search className="h-4 w-4" />
        <span className="text-sm">Search anything…</span>
      </span>
      <kbd className="rounded border border-border-subtle bg-surface px-1.5 py-0.5 font-mono text-[0.65rem] text-text-muted">
        {isMac ? '⌘K' : 'Ctrl+K'}
      </kbd>
    </Button>
  );

  return (
    <>
      {triggerNode}
      <CommandDialog open={open} onOpenChange={setOpen}>
        <CommandInput
          placeholder="Search IP, hash, user, hostname, file, or pages…"
          value={query}
          onValueChange={setQuery}
        />
        <CommandList>
          <CommandEmpty>
            {query.trim() ? (
              <span>
                Press Enter to search <span className="font-mono text-foreground">{query}</span>.
              </span>
            ) : (
              <span>Start typing to search across pages, entities, and saved investigations.</span>
            )}
          </CommandEmpty>

          {detection.type !== 'unknown' && query.trim().length > 1 && (
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
                  <span className="text-sm">Full search results for "{query.trim()}"</span>
                  <CommandShortcut>↵ ⌥</CommandShortcut>
                </CommandItem>
              </CommandGroup>
              <CommandSeparator />
            </>
          )}

          {recents.length > 0 && (
            <>
              <CommandGroup heading="Recent searches">
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
