import { Link, Outlet, useLocation } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import './MainLayout.css';

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard' },
  { to: '/tenants', label: 'Tenants' },
  { to: '/nodes', label: 'Nodes' },
  { to: '/jobs', label: 'Jobs' },
];

export function MainLayout(): JSX.Element {
  const { signOut } = useAuth();
  const location = useLocation();

  return (
    <div className="app-shell">
      <header className="app-header">
        <h1>Control One</h1>
        <nav>
          <ul>
            {NAV_ITEMS.map((item) => (
              <li key={item.to} className={location.pathname === item.to ? 'active' : ''}>
                <Link to={item.to}>{item.label}</Link>
              </li>
            ))}
          </ul>
        </nav>
        <button type="button" onClick={signOut} className="signout-button">
          Sign out
        </button>
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  );
}
