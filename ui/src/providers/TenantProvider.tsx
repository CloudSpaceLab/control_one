import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useAuth } from './AuthProvider';
import type { Tenant } from '@/lib/api';

const STORAGE_KEY = 'co.tenant.id';

interface TenantContextValue {
  tenants: Tenant[];
  loading: boolean;
  error: string | null;
  currentTenantId: string | null;
  currentTenant: Tenant | null;
  setCurrentTenantId: (id: string | null) => void;
  refresh: () => Promise<void>;
}

const TenantContext = createContext<TenantContextValue | undefined>(undefined);

export function TenantProvider({ children }: { children: ReactNode }): JSX.Element {
  const { apiClient, isAuthenticated } = useAuth();
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [currentTenantId, setCurrentTenantIdState] = useState<string | null>(() => {
    if (typeof window === 'undefined') return null;
    return window.localStorage.getItem(STORAGE_KEY);
  });

  const setCurrentTenantId = useCallback((id: string | null) => {
    setCurrentTenantIdState(id);
    if (typeof window !== 'undefined') {
      if (id) window.localStorage.setItem(STORAGE_KEY, id);
      else window.localStorage.removeItem(STORAGE_KEY);
    }
  }, []);

  const refresh = useCallback(async () => {
    if (!isAuthenticated) {
      setTenants([]);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const result = await apiClient.listTenants({ limit: 200, offset: 0 });
      setTenants(result.data);
      if (!currentTenantId && result.data.length > 0) {
        setCurrentTenantId(result.data[0].id);
      } else if (currentTenantId && !result.data.some((t) => t.id === currentTenantId)) {
        setCurrentTenantId(result.data[0]?.id ?? null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load tenants');
    } finally {
      setLoading(false);
    }
  }, [apiClient, isAuthenticated, currentTenantId, setCurrentTenantId]);

  useEffect(() => {
    refresh().catch(() => {});
  }, [isAuthenticated]); // eslint-disable-line react-hooks/exhaustive-deps

  const value = useMemo<TenantContextValue>(
    () => ({
      tenants,
      loading,
      error,
      currentTenantId,
      currentTenant: tenants.find((t) => t.id === currentTenantId) ?? null,
      setCurrentTenantId,
      refresh,
    }),
    [tenants, loading, error, currentTenantId, setCurrentTenantId, refresh],
  );

  return <TenantContext.Provider value={value}>{children}</TenantContext.Provider>;
}

export function useTenant(): TenantContextValue {
  const ctx = useContext(TenantContext);
  if (!ctx) throw new Error('useTenant must be used within a TenantProvider');
  return ctx;
}
