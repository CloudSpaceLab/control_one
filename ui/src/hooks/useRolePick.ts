import { useMemo } from 'react';
import { useAuth } from '@/providers/AuthProvider';

export type RoleName = 'admin' | 'operator' | 'viewer';

const PRIORITY: RoleName[] = ['admin', 'operator', 'viewer'];

function normalize(role: string): RoleName | null {
  const r = role.toLowerCase();
  if (r === 'admin' || r.endsWith(':admin')) return 'admin';
  if (r === 'operator' || r.endsWith(':operator')) return 'operator';
  if (r === 'viewer' || r.endsWith(':viewer')) return 'viewer';
  return null;
}

export function useRolePick(): {
  role: RoleName;
  isAdmin: boolean;
  isOperator: boolean;
  isViewer: boolean;
  hasRole: (r: RoleName) => boolean;
  roles: RoleName[];
} {
  const { profile } = useAuth();
  return useMemo(() => {
    const all = (profile?.roles ?? [])
      .map(normalize)
      .filter((r): r is RoleName => r !== null);
    const set = new Set<RoleName>(all);
    const role = PRIORITY.find((r) => set.has(r)) ?? 'viewer';
    return {
      role,
      isAdmin: set.has('admin'),
      isOperator: set.has('operator'),
      isViewer: set.has('viewer'),
      hasRole: (r: RoleName) => set.has(r),
      roles: PRIORITY.filter((r) => set.has(r)),
    };
  }, [profile?.roles]);
}
