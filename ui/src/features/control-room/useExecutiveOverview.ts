import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ControlRoomExecutiveOverview, Tenant } from '@/lib/api';
import { useApiClient } from '@/hooks/useApiClient';
import { aggregateExecutiveOverviews } from './aggregateExecutiveOverviews';

type TenantScope = Pick<Tenant, 'id' | 'name'>;

export function useExecutiveOverview(
  tenantId: string | null,
  tenants: TenantScope[],
  period: string,
) {
  const api = useApiClient();
  const [overview, setOverview] = useState<ControlRoomExecutiveOverview | null>(null);
  const [loading, setLoading] = useState(Boolean(tenantId) || tenants.length > 0);
  const [error, setError] = useState<string | null>(null);
  const requestId = useRef(0);
  const tenantScope = useMemo(
    () => [...tenants].sort((a, b) => a.id.localeCompare(b.id)),
    [tenants],
  );
  const scopeKey = useMemo(
    () => tenantId ?? `all:${tenantScope.map((tenant) => tenant.id).join(',')}`,
    [tenantId, tenantScope],
  );
  const activeScopeKey = useRef(scopeKey);

  const refresh = useCallback(async () => {
    const nextRequestId = ++requestId.current;
    activeScopeKey.current = scopeKey;

    if (!tenantId && tenantScope.length === 0) {
      setOverview(null);
      setError(null);
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      if (tenantId) {
        const next = await api.getControlRoomExecutiveOverview(tenantId, period);
        if (nextRequestId !== requestId.current || activeScopeKey.current !== scopeKey) return;
        setOverview(next);
        return;
      }

      const results = await Promise.allSettled(
        tenantScope.map(async (tenant) => ({
          tenant,
          overview: await api.getControlRoomExecutiveOverview(tenant.id, period),
        })),
      );
      if (nextRequestId !== requestId.current || activeScopeKey.current !== scopeKey) return;

      const successful = results.flatMap((result) =>
        result.status === 'fulfilled'
          ? [{
              tenantId: result.value.tenant.id,
              tenantName: result.value.tenant.name,
              overview: result.value.overview,
            }]
          : [],
      );
      const failedTenantCount = results.length - successful.length;

      if (successful.length === 0) {
        setOverview(null);
        setError('Dashboard unavailable');
        return;
      }

      setOverview(aggregateExecutiveOverviews(successful, period, failedTenantCount));
      if (failedTenantCount > 0) {
        setError(
          `${failedTenantCount} tenant ${failedTenantCount === 1 ? 'dashboard is' : 'dashboards are'} unavailable.`,
        );
      }
    } catch (err) {
      if (nextRequestId !== requestId.current || activeScopeKey.current !== scopeKey) return;
      setError(err instanceof Error && err.message ? err.message : 'Dashboard unavailable');
      setOverview((current) => {
        const expectedTenantId = tenantId ?? 'all';
        return current?.tenant_id === expectedTenantId && current.period === period ? current : null;
      });
    } finally {
      if (nextRequestId === requestId.current && activeScopeKey.current === scopeKey) {
        setLoading(false);
      }
    }
  }, [api, period, scopeKey, tenantId, tenantScope]);

  useEffect(() => {
    setOverview((current) => {
      const expectedTenantId = tenantId ?? 'all';
      return current?.tenant_id === expectedTenantId && current.period === period ? current : null;
    });
    void refresh();

    return () => {
      requestId.current += 1;
    };
  }, [refresh, tenantId, period]);

  return {
    overview,
    loading,
    error,
    stale: Boolean(error && overview),
    refresh,
  };
}
