import { useState } from 'react';
import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { useTheme } from '../providers/ThemeProvider';
import './MainLayout.css';

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard', icon: '📊', section: 'main' },
  { to: '/setup', label: 'Setup Wizard', icon: '🚀', section: 'main' },
  { to: '/tenants', label: 'Tenants', icon: '🏢', section: 'infrastructure' },
  { to: '/nodes', label: 'Nodes', icon: '🖥️', section: 'infrastructure' },
  { to: '/jobs', label: 'Jobs', icon: '⚙️', section: 'infrastructure' },
  { to: '/templates', label: 'Templates', icon: '📋', section: 'infrastructure' },
  { to: '/compliance', label: 'Compliance', icon: '✅', section: 'security' },
  { to: '/audit', label: 'Audit Log', icon: '📝', section: 'security' },
  { to: '/users', label: 'Users & Roles', icon: '👥', section: 'security' },
  { to: '/telemetry', label: 'Telemetry', icon: '📈', section: 'monitoring' },
  { to: '/secrets', label: 'Secrets', icon: '🔐', section: 'security' },
  { to: '/settings', label: 'Settings', icon: '⚙️', section: 'admin' },
];

const NAV_SECTIONS = [
  { id: 'main', title: 'Main' },
  { id: 'infrastructure', title: 'Infrastructure' },
  { id: 'security', title: 'Security' },
  { id: 'monitoring', title: 'Monitoring' },
  { id: 'admin', title: 'Admin' },
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
          {NAV_SECTIONS.map((section) => (
            <div key={section.id} className="nav-section">
              <div className="nav-section-title">{section.title}</div>
              <ul>
                {NAV_ITEMS.filter((item) => item.section === section.id).map((item) => {
                  const isActive =
                    item.to === '/' ? location.pathname === item.to : location.pathname.startsWith(item.to);
                  return (
                    <li key={item.to} className={isActive ? 'active' : ''}>
                      <Link to={item.to}>
                        <span className="nav-icon">{item.icon}</span>
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
