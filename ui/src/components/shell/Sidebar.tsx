import { useEffect, type ReactNode } from 'react';
import {
  Activity,
  AlertTriangle,
  FileText,
  KeyRound,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Server,
  ShieldAlert,
  type LucideIcon,
} from 'lucide-react';
import { Link, NavLink } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { useLocalStorage } from '@/hooks/useLocalStorage';
import { cn } from '@/lib/utils';
import { NodeStatusBadge } from './NodeStatusBadge';

interface NavItemDef {
  to: string;
  label: string;
  icon: LucideIcon;
  roles?: string[];
  badge?: ReactNode;
  /** Hide unless an env feature flag is set (window.__C1_FLAGS__). */
  flag?: string;
}

interface NavGroupDef {
  label: string;
  items: NavItemDef[];
}

// Primary IA is intentionally narrow: daily operators land on the control room,
// then drill into evidence/configuration from the relevant lane or detail page.
// Settings / Tenants / Secrets / Onboard live in the ProfileMenu, not the sidebar.
const NAV_GROUPS: NavGroupDef[] = [
  {
    label: 'Home',
    items: [
      { to: '/', label: 'Control Room', icon: Activity },
      { to: '/alerts', label: 'Alerts', icon: AlertTriangle },
    ],
  },
  {
    label: 'Investigate',
    items: [
      { to: '/investigate', label: 'Search & lifecycle', icon: Search },
    ],
  },
  {
    label: 'Operations',
    items: [
      {
        to: '/nodes',
        label: 'Servers',
        icon: Server,
        badge: <NodeStatusBadge />,
      },
      { to: '/security/network', label: 'Network & exposure', icon: Network },
      {
        to: '/infrastructure/patch',
        label: 'Patch posture',
        icon: ShieldAlert,
        roles: ['admin', 'operator'],
      },
    ],
  },
  {
    label: 'Governance',
    items: [
      { to: '/compliance', label: 'Compliance', icon: ShieldAlert },
      { to: '/access', label: 'Access', icon: KeyRound },
      { to: '/audit', label: 'Audit log', icon: FileText },
    ],
  },
];

function isFlagEnabled(flag: string): boolean {
  if (typeof window === 'undefined') return false;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const flags = (window as any).__C1_FLAGS__ as Record<string, boolean> | undefined;
  return flags?.[flag] === true;
}

function filterGroups(groups: NavGroupDef[], userRoles: string[]): NavGroupDef[] {
  const isAdmin = userRoles.includes('admin');
  return groups
    .map((g) => ({
      label: g.label,
      items: g.items.filter((item) => {
        if (item.flag && !isFlagEnabled(item.flag)) return false;
        if (!item.roles || item.roles.length === 0) return true;
        if (isAdmin) return true;
        return item.roles.some((r) => userRoles.includes(r));
      }),
    }))
    .filter((g) => g.items.length > 0);
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

export interface SidebarProps {
  userRoles: string[];
  /** When true, render in a slide-out sheet (mobile) — always expanded, no rail. */
  variant?: 'desktop' | 'sheet';
  onNavigate?: () => void;
}

interface NavRowProps {
  item: NavItemDef;
  collapsed: boolean;
  onNavigate?: () => void;
}

function NavRow({ item, collapsed, onNavigate }: NavRowProps) {
  const Icon = item.icon;
  const link = (
    <NavLink
      to={item.to}
      end={item.to === '/'}
      onClick={onNavigate}
      className={({ isActive }) =>
        cn(
          'group relative flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-text-secondary transition-colors hover:bg-hover hover:text-foreground',
          isActive && 'bg-brand-500/10 text-foreground',
          collapsed && 'justify-center px-0',
        )
      }
    >
      {({ isActive }) => (
        <>
          {isActive && (
            <span
              aria-hidden
              className="absolute inset-y-1 left-0 w-0.5 rounded-full bg-brand-400"
            />
          )}
          <Icon
            className={cn(
              'h-4 w-4 shrink-0 text-text-muted transition-colors group-hover:text-foreground',
              isActive && 'text-brand-400',
            )}
          />
          {!collapsed && (
            <>
              <span className="flex-1 truncate">{item.label}</span>
              {item.badge}
            </>
          )}
          {collapsed && item.badge && (
            <span className="absolute right-1 top-1">{item.badge}</span>
          )}
        </>
      )}
    </NavLink>
  );

  if (!collapsed) return link;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{link}</TooltipTrigger>
      <TooltipContent side="right" className="font-display text-xs">
        {item.label}
      </TooltipContent>
    </Tooltip>
  );
}

export function Sidebar({ userRoles, variant = 'desktop', onNavigate }: SidebarProps) {
  const [pinned, setPinned] = useLocalStorage<boolean>('co.sidebar.pinned', true);
  const collapsed = variant === 'desktop' && !pinned;

  // [-key shortcut to toggle pinned/rail. Ignore when typing.
  useEffect(() => {
    if (variant !== 'desktop') return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== '[') return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTypingTarget(e.target)) return;
      e.preventDefault();
      setPinned((p) => !p);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [variant, setPinned]);

  const groups = filterGroups(NAV_GROUPS, userRoles);

  const isSheet = variant === 'sheet';
  const widthClass = isSheet
    ? 'w-full'
    : collapsed
      ? 'w-[var(--rail-width)]'
      : 'w-[var(--sidebar-pinned-width)]';

  return (
    <TooltipProvider delayDuration={200}>
      <aside
        className={cn(
          'group/sidebar sticky top-0 z-40 flex h-screen shrink-0 flex-col border-r border-border-subtle bg-surface backdrop-blur',
          isSheet ? 'flex' : 'hidden md:flex',
          widthClass,
          'transition-[width] duration-200',
        )}
        aria-label="Primary navigation"
        data-collapsed={collapsed ? 'true' : 'false'}
      >
        <div
          className={cn(
            'flex items-center gap-2 px-3 py-3.5',
            collapsed && 'justify-center px-2',
          )}
        >
          <Link
            to="/"
            onClick={onNavigate}
            className="inline-flex items-center gap-2 text-foreground"
            aria-label="Control One home"
          >
            <span
              className="grid h-7 w-7 place-items-center rounded-md bg-gradient-to-br from-brand-500 to-accent-500 font-display text-sm font-bold text-[#0f172a]"
              aria-hidden
            >
              ◎
            </span>
            {!collapsed && (
              <span className="font-display text-sm font-semibold tracking-tight">
                Control One
              </span>
            )}
          </Link>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-4">
          {groups.map((group) => (
            <div key={group.label} className="mb-3">
              {!collapsed && (
                <div className="px-3 pb-1 font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
                  {group.label}
                </div>
              )}
              <ul className="flex flex-col gap-0.5">
                {group.items.map((item) => (
                  <li key={item.to}>
                    <NavRow item={item} collapsed={collapsed} onNavigate={onNavigate} />
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>

        {!isSheet && (
          <div
            className={cn(
              'border-t border-border-subtle p-2',
              collapsed && 'flex justify-center',
            )}
          >
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setPinned((p) => !p)}
                  aria-label={collapsed ? 'Pin sidebar (])' : 'Collapse sidebar ([)'}
                  className="h-8 w-8"
                >
                  {collapsed ? (
                    <PanelLeftOpen className="h-4 w-4" />
                  ) : (
                    <PanelLeftClose className="h-4 w-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right" className="font-display text-xs">
                {collapsed ? 'Expand' : 'Collapse'}
                <kbd className="ml-2 inline-flex h-4 items-center rounded border border-border-subtle bg-surface px-1 font-mono text-[0.6rem]">
                  [
                </kbd>
              </TooltipContent>
            </Tooltip>
          </div>
        )}
      </aside>
    </TooltipProvider>
  );
}
