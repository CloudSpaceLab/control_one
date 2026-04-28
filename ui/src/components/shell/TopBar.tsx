import { Menu } from 'lucide-react';
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
    <header className="sticky top-0 z-30 flex h-14 w-full items-center gap-3 border-b border-border-subtle bg-surface/80 px-3 backdrop-blur sm:px-5">
      {mobileNav && (
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon" className="md:hidden" aria-label="Open navigation">
              <Menu className="h-4 w-4" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="p-0 w-[280px]">
            {mobileNav}
          </SheetContent>
        </Sheet>
      )}

      <div className="flex flex-1 items-center justify-center max-w-md">
        <GlobalSearch />
      </div>

      <div className="ml-auto flex items-center gap-2">
        <TenantSwitcher />
        <LiveBadge state={liveState} />
        <ThemeToggle />
        <ProfileMenu />
      </div>
    </header>
  );
}
