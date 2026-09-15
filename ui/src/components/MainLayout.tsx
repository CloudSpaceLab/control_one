import { Link, Outlet, useLocation } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { useAuth } from '../providers/AuthProvider';
import { Sidebar } from './shell/Sidebar';
import { TopBar } from './shell/TopBar';
import { CommandPalette } from './CommandPalette';

function ReturnToAlertChip(): JSX.Element | null {
  const location = useLocation();
  const fromAlert = new URLSearchParams(location.search).get('fromAlert');
  if (!fromAlert || location.pathname === '/alerts') return null;

  return (
    <Link
      to="/alerts"
      className="fixed bottom-4 right-4 z-50 inline-flex items-center gap-2 rounded-md border border-border-strong bg-surface px-3 py-2 text-sm font-medium text-foreground shadow-lg transition hover:bg-surface-2"
    >
      <ArrowLeft className="h-4 w-4" />
      Return to alert triage
    </Link>
  );
}

export function MainLayout(): JSX.Element {
  const { profile } = useAuth();
  const userRoles = profile?.roles ?? [];

  return (
    <div className="flex min-h-screen w-full bg-canvas">
      <CommandPalette />
      <Sidebar userRoles={userRoles} />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar mobileNav={<Sidebar userRoles={userRoles} variant="sheet" />} />
        <main className="flex-1 overflow-x-hidden px-4 py-5 sm:px-6 lg:px-8">
          <Outlet />
        </main>
      </div>
      <ReturnToAlertChip />
    </div>
  );
}
