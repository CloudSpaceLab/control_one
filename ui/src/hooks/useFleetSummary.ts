import { useEffect, useState } from 'react';
import type { FleetHealthSnapshot, NodeSummary } from '../lib/api';
import { useApiClient } from './useApiClient';

interface Options {
  tenantId?: string;
  since?: string;
  // Refresh interval in ms. Default 30 s. SSE-driven invalidation could
  // replace polling later.
  intervalMs?: number;
}

function fallbackTotals(nodes: NodeSummary[], total: number): FleetHealthSnapshot['totals'] {
  const totals: FleetHealthSnapshot['totals'] = {
    nodes: total,
    healthy: 0,
    warning: 0,
    degraded: 0,
    critical: 0,
    unknown: Math.max(0, total - nodes.length),
  };

  for (const node of nodes) {
    switch ((node.state ?? '').toLowerCase()) {
      case 'active':
        totals.healthy += 1;
        break;
      case 'enrollment_pending':
        totals.warning += 1;
        break;
      case 'enrollment_failed':
        totals.critical += 1;
        break;
      default:
        totals.unknown += 1;
        break;
    }
  }

  return totals;
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
        let snap = await api.fleetHealthSnapshot({
          tenantId: opts.tenantId,
          since: opts.since,
        });
        if ((snap.totals?.nodes ?? 0) === 0 && opts.tenantId) {
          try {
            const nodePage = await api.listNodes({ tenantId: opts.tenantId, limit: 500, offset: 0 });
            if ((nodePage.pagination.total ?? 0) > 0 || nodePage.data.length > 0) {
              snap = {
                ...snap,
                source: 'postgres-fallback',
                totals: fallbackTotals(nodePage.data, nodePage.pagination.total || nodePage.data.length),
              };
            }
          } catch {
            // Keep the original health snapshot if the best-effort fallback fails.
          }
        }
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
