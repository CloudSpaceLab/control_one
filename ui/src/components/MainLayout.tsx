import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { useTheme } from '../providers/ThemeProvider';
import './MainLayout.css';

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard' },
  { to: '/tenants', label: 'Tenants' },
  { to: '/nodes', label: 'Nodes' },
  { to: '/fleet-enroll', label: 'Fleet Enroll' },
  { to: '/hypervisors', label: 'Hypervisors' },
  { to: '/jobs', label: 'Jobs' },
  { to: '/templates', label: 'Templates' },
  { to: '/compliance', label: 'Compliance' },
  { to: '/audit', label: 'Audit Log' },
  { to: '/users', label: 'Users & Roles' },
  { to: '/telemetry', label: 'Telemetry' },
  { to: '/secrets', label: 'Secrets' },
  { to: '/offline-bundle', label: 'Offline Bundle' },
  { to: '/settings', label: 'Settings' },
];

export function MainLayout(): JSX.Element {
  const { signOut } = useAuth();
  const { theme, toggleTheme } = useTheme();
  const location = useLocation();
  const [navOpen, setNavOpen] = useState(false);

  return (
    <div className="app-shell">
      <aside className={`side-nav ${navOpen ? 'open' : ''}`}>
        <div className="brand">
          <span className="brand-mark">◎</span>
          <div>
            <strong>Control One</strong>
            <small>Operator</small>
          </div>
        </div>
        <nav>
          <ul>
            {NAV_ITEMS.map((item) => {
              const isActive =
                item.to === '/' ? location.pathname === item.to : location.pathname.startsWith(item.to);
              return (
                <li key={item.to} className={isActive ? 'active' : ''}>
                  <Link to={item.to}>{item.label}</Link>
                </li>
              );
            })}
          </ul>
        </nav>
        <footer>
          <p>Secure • Multi-tenant</p>
        </footer>
      </aside>
      {navOpen ? <div className="nav-backdrop" onClick={() => setNavOpen(false)} /> : null}
      <div className="content-area">
        <header className="content-header">
          <div>
            <p className="eyebrow">Unified control plane</p>
            <h1>Operator Console</h1>
            <p className="subtitle">Provision infrastructure, enforce policy, and monitor node posture.</p>
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
