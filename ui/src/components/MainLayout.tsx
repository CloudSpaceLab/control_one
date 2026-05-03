import { Outlet } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import { Sidebar } from './shell/Sidebar';
import { TopBar } from './shell/TopBar';
import { CommandPalette } from './CommandPalette';

export function MainLayout(): JSX.Element {
  const { profile } = useAuth();
  const userRoles = profile?.roles ?? [];

  return (
    <div className="flex min-h-screen w-full bg-canvas">
      <CommandPalette />
      <Sidebar userRoles={userRoles} />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar mobileNav={<Sidebar userRoles={userRoles} variant="sheet" />} />
        <main className="flex-1 overflow-x-hidden">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
