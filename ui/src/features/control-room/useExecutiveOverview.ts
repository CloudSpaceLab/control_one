import { useCallback, useEffect, useState } from 'react';
import type { ControlRoomExecutiveOverview } from '@/lib/api';
import { useApiClient } from '@/hooks/useApiClient';

export function useExecutiveOverview(tenantId: string | null, period: string) {
  const api = useApiClient();
  const [overview, setOverview] = useState<ControlRoomExecutiveOverview | null>(null);
  const [loading, setLoading] = useState(Boolean(tenantId));
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!tenantId) {
      setOverview(null);
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const next = await api.getControlRoomExecutiveOverview(tenantId, period);
      setOverview(next);
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : 'Dashboard unavailable');
      setOverview((current) => (
        current?.tenant_id === tenantId && current.period === period ? current : null
      ));
    } finally {
      setLoading(false);
    }
  }, [api, tenantId, period]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return {
    overview,
    loading,
    error,
    stale: Boolean(error && overview),
    refresh,
  };
}
