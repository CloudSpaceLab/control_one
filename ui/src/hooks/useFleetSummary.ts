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
    // Lifecycle state only says that a node is registered.  A node that has
    // stopped heartbeating must not be counted as healthy just because its
    // lifecycle state is still `active`.
    const lastSeen = node.last_seen_at ? new Date(node.last_seen_at).getTime() : Number.NaN;
    const online = Number.isFinite(lastSeen) && Date.now() - lastSeen < 5 * 60 * 1000;
    switch ((node.state ?? '').toLowerCase()) {
      case 'active':
        if (online) totals.healthy += 1;
        else totals.warning += 1;
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

function hasConsistentTotals(snapshot: FleetHealthSnapshot, nodeCount: number): boolean {
  const totals = snapshot.totals;
  const classified = totals.healthy + totals.warning + totals.degraded + totals.critical + totals.unknown;
  return totals.nodes === nodeCount && classified === nodeCount;
}

export function useFleetSummary(opts: Options = {}) {
  const api = useApiClient();
  const [data, setData] = useState<FleetHealthSnapshot | null>(null);
  const [nodes, setNodes] = useState<NodeSummary[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    if (!opts.tenantId) {
      setData(null);
      setNodes([]);
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
        try {
          const nodePage = await api.listNodes({ tenantId: opts.tenantId, limit: 500, offset: 0 });
          if (!cancelled) setNodes(nodePage.data);
          const nodeCount = nodePage.pagination.total || nodePage.data.length;
          if (!hasConsistentTotals(snap, nodeCount) && nodeCount > 0) {
            snap = {
              ...snap,
              source: 'postgres-fallback',
              totals: fallbackTotals(nodePage.data, nodeCount),
            };
          }
        } catch {
          // Keep the original health snapshot if the best-effort node lookup fails.
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

  return { data, nodes, loading, error };
}
