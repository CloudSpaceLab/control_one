import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { useTheme } from '../providers/ThemeProvider';
import { CommandPalette } from './CommandPalette';
import './MainLayout.css';

interface NavItem {
  to: string;
  label: string;
  roles?: string[];
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

// Grouped IA: 7 sections, each with a clear workflow. Inside each group items
// are ordered by frequency, not alphabetically.
const NAV_GROUPS: NavGroup[] = [
  {
    label: 'Get started',
    items: [
      { to: '/onboard', label: 'Onboard a server', roles: ['admin', 'operator'] },
    ],
  },
  {
    label: 'Investigate',
    items: [
      { to: '/investigate', label: 'Search & lifecycle' },
      { to: '/investigate/saved', label: 'Saved searches' },
    ],
  },
  {
    label: 'Visibility',
    items: [
      { to: '/', label: 'Dashboard' },
      { to: '/alerts', label: 'Alerts' },
      { to: '/reports', label: 'Reports' },
    ],
  },
  {
    label: 'Posture',
    items: [
      { to: '/compliance', label: 'Compliance' },
      { to: '/compliance-evidence', label: 'Evidence', roles: ['admin', 'operator'] },
      { to: '/audit-reports', label: 'Audit reports', roles: ['admin', 'operator'] },
      { to: '/frameworks', label: 'Frameworks' },
      { to: '/audit', label: 'Audit log' },
      { to: '/telemetry', label: 'Telemetry' },
    ],
  },
  {
    label: 'Detect & respond',
    items: [
      { to: '/rules', label: 'Rules', roles: ['admin', 'operator'] },
      { to: '/threat-feeds', label: 'Threat sources', roles: ['admin', 'operator'] },
      { to: '/connections', label: 'Connections' },
      { to: '/dashboards', label: 'Custom dashboards' },
      { to: '/recommendations', label: 'Recommendations' },
    ],
  },
  {
    label: 'Access',
    items: [
      { to: '/access', label: 'Just-in-time access' },
      { to: '/sessions', label: 'Session replay' },
      { to: '/users', label: 'Users', roles: ['admin'] },
      { to: '/roles', label: 'Roles & permissions', roles: ['admin'] },
    ],
  },
  {
    label: 'Infrastructure',
    items: [
      { to: '/nodes', label: 'Nodes' },
      { to: '/fleet-enroll', label: 'Fleet enrol', roles: ['admin', 'operator'] },
      { to: '/hypervisors', label: 'Hypervisors', roles: ['admin'] },
      { to: '/templates', label: 'Templates', roles: ['admin', 'operator'] },
    ],
  },
  {
    label: 'Automation',
    items: [
      { to: '/jobs', label: 'Jobs' },
      { to: '/offline-bundle', label: 'Offline bundle', roles: ['admin', 'operator'] },
    ],
  },
  {
    label: 'Configuration',
    items: [
      { to: '/tenants', label: 'Tenants', roles: ['admin'] },
      { to: '/secrets', label: 'Secrets', roles: ['admin'] },
      { to: '/settings', label: 'Settings', roles: ['admin'] },
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

export function MainLayout(): JSX.Element {
  const { signOut, profile } = useAuth();
  const { theme, toggleTheme } = useTheme();
  const location = useLocation();
  const [navOpen, setNavOpen] = useState(false);

  const userRoles = profile?.roles ?? [];
  const groups = filterGroups(NAV_GROUPS, userRoles);
  const isMac = navigator.platform.toUpperCase().includes('MAC');

  return (
    <div className="app-shell">
      <CommandPalette />
      <aside className={`side-nav ${navOpen ? 'open' : ''}`}>
        <div className="brand">
          <span className="brand-mark">◎</span>
          <div>
            <strong>Control One</strong>
            <small>Operator console</small>
          </div>
        </div>
        <nav aria-label="Primary">
          {groups.map((group) => (
            <div className="nav-group" key={group.label}>
              <div className="nav-group__label">{group.label}</div>
              <ul>
                {group.items.map((item) => {
                  const isActive =
                    item.to === '/'
                      ? location.pathname === item.to
                      : location.pathname.startsWith(item.to);
                  return (
                    <li key={item.to} className={isActive ? 'active' : ''}>
                      <Link to={item.to} onClick={() => setNavOpen(false)}>
                        {item.label}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </nav>
        <footer>
          <button
            type="button"
            className="cmdk-hint"
            aria-label="Open command palette"
            onClick={() => {
              window.dispatchEvent(
                new KeyboardEvent('keydown', {
                  key: 'k',
                  [isMac ? 'metaKey' : 'ctrlKey']: true,
                  bubbles: true,
                }),
              );
            }}
          >
            <span>Quick search</span>
            <kbd>{isMac ? '⌘K' : 'Ctrl+K'}</kbd>
          </button>
        </footer>
      </aside>
      {navOpen ? (
        <div className="nav-backdrop" onClick={() => setNavOpen(false)} />
      ) : null}
      <div className="content-area">
        <header className="content-header">
          <div>
            <p className="eyebrow">{profile?.user?.email ?? 'Signed in'}</p>
            <h1>Control One</h1>
            <p className="subtitle">Find risk. Fix it. Prove it.</p>
          </div>
          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
            <button
              type="button"
              onClick={toggleTheme}
              className="theme-toggle"
              aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`}
              title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`}
            >
              {theme === 'dark' ? '☀️' : '🌙'}
            </button>
            <button type="button" onClick={signOut} className="signout-button">
              Sign out
            </button>
          </div>
          <button
            type="button"
            className="nav-toggle"
            aria-label="Toggle navigation"
            onClick={() => setNavOpen((open) => !open)}
          >
            <span />
            <span />
            <span />
          </button>
        </header>
        <main className="app-main">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
