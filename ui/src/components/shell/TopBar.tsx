import { Menu, Rocket } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet';
import { LiveBadge, type LiveState } from '@/components/kit';
import { GlobalSearch } from './GlobalSearch';
import { ProfileMenu } from './ProfileMenu';
import { TenantSwitcher } from './TenantSwitcher';
import { ThemeToggle } from './ThemeToggle';
import type { ReactNode } from 'react';

export interface TopBarProps {
  liveState?: LiveState;
  mobileNav?: ReactNode;
}

export function TopBar({ liveState = 'live', mobileNav }: TopBarProps) {
  return (
    <header className="sticky top-0 z-30 flex h-14 w-full min-w-0 items-center gap-2 border-b border-border-subtle bg-surface/80 px-2 backdrop-blur sm:gap-3 sm:px-5">
      {mobileNav && (
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="shrink-0 md:hidden" aria-label="Open navigation">
              <Menu className="h-4 w-4" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="p-0 w-[280px]">
            {mobileNav}
          </SheetContent>
        </Sheet>
      )}

      <div className="flex min-w-0 flex-1 items-center justify-center">
        <GlobalSearch />
      </div>

      <div className="ml-auto flex min-w-0 shrink-0 items-center gap-1.5 sm:gap-2">
        <Button asChild variant="secondary" size="sm" className="hidden h-9 px-2 sm:inline-flex sm:px-3">
          <Link to="/onboard" aria-label="Open enrollment" title="Open enrollment">
            <Rocket className="h-4 w-4" />
            <span className="hidden sm:inline">Enroll</span>
          </Link>
        </Button>
        <TenantSwitcher />
        <LiveBadge state={liveState} />
        <div className="hidden sm:block">
          <ThemeToggle />
        </div>
        <ProfileMenu />
      </div>
    </header>
  );
}
