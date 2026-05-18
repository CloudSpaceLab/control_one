import {
  Boxes,
  Building2,
  KeyRound,
  LogOut,
  PackageCheck,
  Rocket,
  Server,
  Settings,
  User as UserIcon,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { useAuth } from '@/providers/AuthProvider';
import { useRolePick } from '@/hooks/useRolePick';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

export function ProfileMenu() {
  const { profile, signOut } = useAuth();
  const { role } = useRolePick();
  const initial = (profile?.name || profile?.email || '?').charAt(0).toUpperCase();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9 rounded-full bg-surface-2 font-semibold text-foreground"
          aria-label="Profile menu"
        >
          {initial}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>
          <div className="flex flex-col gap-0.5">
            <span className="font-display text-sm text-foreground normal-case tracking-normal">
              {profile?.name || 'Signed in'}
            </span>
            <span className="text-[0.7rem] text-text-muted normal-case tracking-normal">
              {profile?.email}
            </span>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-[0.6rem]">Role</DropdownMenuLabel>
        <DropdownMenuItem className="font-mono text-xs uppercase">{role}</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/settings">
            <Settings className="h-4 w-4" />
            Settings
          </Link>
        </DropdownMenuItem>
        {(role === 'admin') && (
          <>
            <DropdownMenuItem asChild>
              <Link to="/tenants">
                <Building2 className="h-4 w-4" />
                Tenants
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link to="/secrets">
                <KeyRound className="h-4 w-4" />
                Secrets
              </Link>
            </DropdownMenuItem>
          </>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-[0.6rem]">Enrollment</DropdownMenuLabel>
        <DropdownMenuItem asChild>
          <Link to="/onboard">
            <Rocket className="h-4 w-4" />
            Onboarding
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/fleet-enroll">
            <Server className="h-4 w-4" />
            Bulk server enrollment
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/hypervisors">
            <Boxes className="h-4 w-4" />
            Hypervisors and cloud
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/offline-bundle">
            <PackageCheck className="h-4 w-4" />
            Offline bundles
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/users">
            <UserIcon className="h-4 w-4" />
            Account
          </Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={signOut} className="text-state-critical focus:text-state-critical">
          <LogOut className="h-4 w-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
