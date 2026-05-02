import { ChevronLeft, ChevronRight, type LucideIcon } from 'lucide-react';
import {
  Activity,
  AlertTriangle,
  Boxes,
  Building2,
  FileText,
  KeyRound,
  Layers,
  Network,
  Search,
  Server,
  ShieldAlert,
  Sliders,
  Terminal,
  User as UserIcon,
  Workflow,
} from 'lucide-react';
import { Link, NavLink } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { useLocalStorage } from '@/hooks/useLocalStorage';
import { cn } from '@/lib/utils';

interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  roles?: string[];
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    label: 'Investigate',
    items: [
      { to: '/investigate', label: 'Search & lifecycle', icon: Search },
      { to: '/investigate/saved', label: 'Saved searches', icon: FileText },
    ],
  },
  {
    label: 'Visibility',
    items: [
      { to: '/', label: 'Dashboard', icon: Activity },
      { to: '/alerts', label: 'Alerts', icon: AlertTriangle },
      { to: '/reports', label: 'Reports', icon: FileText },
    ],
  },
  {
    label: 'Posture',
    items: [
      { to: '/compliance', label: 'Compliance', icon: ShieldAlert },
      { to: '/audit', label: 'Audit log', icon: FileText },
      { to: '/telemetry', label: 'Telemetry', icon: Activity },
    ],
  },
  {
    label: 'Detect & respond',
    items: [
      { to: '/rules', label: 'Rules', icon: ShieldAlert, roles: ['admin', 'operator'] },
      { to: '/security/network', label: 'Network security', icon: Network },
      { to: '/dashboards', label: 'Custom dashboards', icon: Layers },
      { to: '/recommendations', label: 'Recommendations', icon: ShieldAlert },
    ],
  },
  {
    label: 'Access',
    items: [
      { to: '/access', label: 'Just-in-time access', icon: KeyRound },
      { to: '/sessions', label: 'Session replay', icon: Terminal },
      { to: '/users', label: 'Users', icon: UserIcon, roles: ['admin'] },
      { to: '/roles', label: 'Roles & permissions', icon: UserIcon, roles: ['admin'] },
    ],
  },
  {
    label: 'Infrastructure',
    items: [
      { to: '/nodes', label: 'Nodes', icon: Server },
      { to: '/fleet-enroll', label: 'Fleet enrol', icon: Server, roles: ['admin', 'operator'] },
      { to: '/hypervisors', label: 'Hypervisors', icon: Boxes, roles: ['admin'] },
      { to: '/templates', label: 'Templates', icon: FileText, roles: ['admin', 'operator'] },
      { to: '/infrastructure/patch', label: 'Patch management', icon: ShieldAlert, roles: ['admin', 'operator'] },
    ],
  },
  {
    label: 'Automation',
    items: [
      { to: '/jobs', label: 'Jobs', icon: Workflow },
      { to: '/offline-bundle', label: 'Offline bundle', icon: FileText, roles: ['admin', 'operator'] },
    ],
  },
  {
    label: 'Configuration',
    items: [
      { to: '/tenants', label: 'Tenants', icon: Building2, roles: ['admin'] },
      { to: '/secrets', label: 'Secrets', icon: KeyRound, roles: ['admin'] },
      { to: '/settings', label: 'Settings', icon: Sliders, roles: ['admin'] },
    ],
  },
];

function filterGroups(groups: NavGroup[], userRoles: string[]): NavGroup[] {
  const isAdmin = userRoles.includes('admin');
  return groups
    .map((g) => ({
      label: g.label,
      items: g.items.filter((item) => {
        if (!item.roles || item.roles.length === 0) return true;
        if (isAdmin) return true;
        return item.roles.some((r) => userRoles.includes(r));
      }),
    }))
    .filter((g) => g.items.length > 0);
}

export interface SidebarProps {
  userRoles: string[];
}

export function Sidebar({ userRoles }: SidebarProps) {
  const [collapsed, setCollapsed] = useLocalStorage<boolean>('co.sidebar.collapsed', false);
  const groups = filterGroups(NAV_GROUPS, userRoles);

  return (
    <TooltipProvider delayDuration={200}>
      <aside
        className={cn(
          'sticky top-0 z-40 hidden h-screen shrink-0 flex-col border-r border-border-subtle bg-surface backdrop-blur md:flex',
          collapsed ? 'w-[72px]' : 'w-[240px]',
          'transition-[width] duration-200',
        )}
        aria-label="Primary navigation"
      >
        <div className={cn('flex items-center gap-2 px-4 py-4', collapsed && 'justify-center px-2')}>
          <Link
            to="/"
            className="inline-flex items-center gap-2 text-foreground"
            aria-label="Control One home"
          >
            <span
              className="grid h-8 w-8 place-items-center rounded-md bg-gradient-to-br from-brand-500 to-accent-500 font-display text-sm font-bold text-[#0f172a]"
              aria-hidden
            >
              ◎
            </span>
            {!collapsed && (
              <span className="font-display text-sm font-semibold tracking-tight">Control One</span>
            )}
          </Link>
        </div>

        <nav className="flex-1 overflow-y-auto px-2 pb-4">
          {groups.map((group) => (
            <div key={group.label} className="mb-4">
              {!collapsed && (
                <div className="px-3 pb-1.5 font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
                  {group.label}
                </div>
              )}
              <ul className="flex flex-col gap-0.5">
                {group.items.map((item) => {
                  const Icon = item.icon;
                  const link = (
                    <NavLink
                      to={item.to}
                      end={item.to === '/'}
                      className={({ isActive }) =>
                        cn(
                          'group flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-text-secondary transition-colors hover:bg-hover hover:text-foreground',
                          isActive && 'bg-brand-500/10 text-foreground',
                          collapsed && 'justify-center px-0',
                        )
                      }
                    >
                      {({ isActive }) => (
                        <>
                          <Icon
                            className={cn(
                              'h-4 w-4 shrink-0 text-text-muted group-hover:text-foreground',
                              isActive && 'text-brand-400',
                            )}
                          />
                          {!collapsed && <span className="truncate">{item.label}</span>}
                        </>
                      )}
                    </NavLink>
                  );
                  return (
                    <li key={item.to}>
                      {collapsed ? (
                        <Tooltip>
                          <TooltipTrigger asChild>{link}</TooltipTrigger>
                          <TooltipContent side="right">{item.label}</TooltipContent>
                        </Tooltip>
                      ) : (
                        link
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </nav>

        <div className={cn('border-t border-border-subtle p-2', collapsed && 'flex justify-center')}>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setCollapsed((c) => !c)}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
          </Button>
        </div>
      </aside>
    </TooltipProvider>
  );
}
