import { useEffect, useState } from 'react';
import { FleetHealthSnapshot } from '../lib/api';
import { useApiClient } from './useApiClient';

interface Options {
  tenantId?: string;
  since?: string;
  // Refresh interval in ms. Default 30 s. SSE-driven invalidation could
  // replace polling later.
  intervalMs?: number;
}

export function useFleetSummary(opts: Options = {}) {
  const api = useApiClient();
  const [data, setData] = useState<FleetHealthSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    if (!opts.tenantId) {
      setData(null);
      setError(null);
      setLoading(false);
      return () => {
        cancelled = true;
      };
    }

    const tick = async () => {
      try {
        const snap = await api.fleetHealthSnapshot({
          tenantId: opts.tenantId,
          since: opts.since,
        });
        if (!cancelled) {
          setData(snap);
          setLoading(false);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err as Error);
          setLoading(false);
        }
      } finally {
        if (!cancelled) {
          timer = setTimeout(tick, opts.intervalMs ?? 30000);
        }
      }
    };

    tick();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [api, opts.tenantId, opts.since, opts.intervalMs]);

  return { data, loading, error };
}
