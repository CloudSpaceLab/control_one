import { useEffect, useState } from 'react';
import { useApiClient } from './useApiClient';
import type { Node, NodeHealthScore, TelemetryMetric } from '../lib/api';

export interface UseNodeOptions {
  pollIntervalMs?: number;
}

export interface UseNodeResult {
  node: Node | null;
  health: NodeHealthScore | null;
  telemetry: TelemetryMetric[];
  loading: boolean;
  error: Error | null;
  reload: () => void;
}

export function useNode(nodeId: string | null | undefined, options?: UseNodeOptions): UseNodeResult {
  const api = useApiClient();
  const [node, setNode] = useState<Node | null>(null);
  const [health, setHealth] = useState<NodeHealthScore | null>(null);
  const [telemetry, setTelemetry] = useState<TelemetryMetric[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [tick, setTick] = useState(0);

  const pollIntervalMs = options?.pollIntervalMs ?? 10_000;

  useEffect(() => {
    if (!nodeId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);

        const since = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();

        const [n, h, t] = await Promise.all([
          api.getNode(nodeId),
          api.getNodeHealth(nodeId).catch(() => null),
          api.getNodeTelemetryMetrics(nodeId, { since, limit: 2000 }).catch(() => ({ data: [] as TelemetryMetric[] })),
        ]);

        if (cancelled) return;
        setNode(n);
        setHealth(h);
        setTelemetry(Array.isArray(t) ? t : t.data ?? []);
      } catch (err) {
        if (!cancelled) setError(err as Error);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    fetchData();

    if (pollIntervalMs && pollIntervalMs > 0) {
      timer = setInterval(fetchData, pollIntervalMs);
    }

    return () => {
      cancelled = true;
      if (timer) {
        clearInterval(timer);
      }
    };
  }, [api, nodeId, tick, pollIntervalMs]);

  return {
    node,
    health,
    telemetry,
    loading,
    error,
    reload: () => setTick((t) => t + 1),
  };
}
